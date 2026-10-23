import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useT } from '../../contexts/I18nContext'
import { useToast } from '../../contexts/ToastContext'
import { logService } from '../../services/logService'
import { getErrorMessage } from '../../services/api'
import { copyText } from '../../utils/clipboard'
import type { LogEntry, LogLevel, LogQuery } from '../../types/logs'
import SearchBar from '../SearchBar'
import { EmptyState, Icon, PageHeader, cx } from '../ui'
import { LogRow } from './LogRow'
import { dayKey, formatBytes, formatDate, parseEntryTime } from './logFormat'
import './LogsPage.css'

type LevelFilter = 'all' | 'warn' | 'error'

const LEVEL_FILTERS: LevelFilter[] = ['all', 'warn', 'error']
const LEVELS: Record<LevelFilter, LogLevel[]> = {
    all: [],
    warn: ['WARN', 'ERROR', 'FATAL'],
    error: ['ERROR', 'FATAL'],
}
const LEVEL_LABEL_KEYS = { all: 'logs.levelAll', warn: 'logs.levelWarn', error: 'logs.levelError' } as const
const HTTP_EVENT_PREFIX = 'http.request.'
const PAGE_SIZE = 200
const FOLLOW_LIMIT = 500
/** Hard cap on rendered rows. Beyond it, older history is reached by filtering, searching or downloading. */
const MAX_ENTRIES = 2000
const POLL_INTERVAL_MS = 2000
const SEARCH_DEBOUNCE_MS = 300
const TOP_THRESHOLD_PX = 48
const PREFS_KEY = 'cornerstone.logs.prefs'

interface Prefs {
    level: LevelFilter
    hideHttp: boolean
    follow: boolean
}

const loadPrefs = (): Prefs => {
    const fallback: Prefs = { level: 'all', hideHttp: true, follow: true }
    try {
        const parsed = JSON.parse(window.localStorage.getItem(PREFS_KEY) || '{}') as Partial<Prefs>
        return {
            level: LEVEL_FILTERS.includes(parsed.level as LevelFilter) ? (parsed.level as LevelFilter) : fallback.level,
            hideHttp: typeof parsed.hideHttp === 'boolean' ? parsed.hideHttp : fallback.hideHttp,
            follow: typeof parsed.follow === 'boolean' ? parsed.follow : fallback.follow,
        }
    } catch {
        return fallback
    }
}

type Phase = 'loading' | 'ready' | 'error'

/** The first row visible in the scroll panel, and where it sits relative to the panel top. */
const captureAnchor = (container: HTMLElement) => {
    const panelTop = container.getBoundingClientRect().top
    for (const row of container.querySelectorAll<HTMLElement>('[data-offset]')) {
        const rect = row.getBoundingClientRect()
        if (rect.bottom > panelTop) return { offset: Number(row.dataset.offset), top: rect.top - panelTop }
    }
    return null
}

/** Entries are kept newest → oldest, the order they are displayed in. */
export default function LogsPage() {
    const { t, locale } = useT()
    const { showToast } = useToast()
    const [prefs, setPrefs] = useState<Prefs>(loadPrefs)
    const [searchInput, setSearchInput] = useState('')
    const [search, setSearch] = useState('')
    const [phase, setPhase] = useState<Phase>('loading')
    const [refreshing, setRefreshing] = useState(false)
    const [loadError, setLoadError] = useState('')
    const [entries, setEntries] = useState<LogEntry[]>([])
    const [hasMore, setHasMore] = useState(false)
    const [loadingOlder, setLoadingOlder] = useState(false)
    const [file, setFile] = useState('')
    const [size, setSize] = useState(0)
    const [expanded, setExpanded] = useState<Set<number>>(() => new Set())
    const [fresh, setFresh] = useState<Set<number>>(() => new Set())
    const [unseen, setUnseen] = useState(0)
    const [notice, setNotice] = useState('')
    const [pollFailed, setPollFailed] = useState(false)
    const [downloading, setDownloading] = useState(false)
    // Incremented only when a poll delivers lines, so the live dot blips because of real data.
    const [blip, setBlip] = useState(0)

    const scrollRef = useRef<HTMLDivElement>(null)
    const sentinelRef = useRef<HTMLDivElement>(null)
    const generation = useRef(0)
    const cursor = useRef(0)
    const nextBefore = useRef(0)
    const polling = useRef(false)
    // Refs close the gap before a render/effect can observe a tail reload.
    const loadingTail = useRef(false)
    const olderRequest = useRef<number | null>(null)
    const atTop = useRef(true)
    const anchor = useRef<{ offset: number; top: number } | null>(null)
    const entriesCount = useRef(0)
    const trimmed = useRef(false)

    useEffect(() => {
        entriesCount.current = entries.length
        if (trimmed.current && entries.length > 0) {
            // Follow mode dropped the oldest rows; the oldest kept row becomes the next page boundary.
            trimmed.current = false
            nextBefore.current = entries[entries.length - 1].offset
            setHasMore(true)
        }
    }, [entries])

    const capped = hasMore && entries.length >= MAX_ENTRIES

    useEffect(() => {
        window.localStorage.setItem(PREFS_KEY, JSON.stringify(prefs))
    }, [prefs])

    useEffect(() => {
        const timer = window.setTimeout(() => setSearch(searchInput.trim()), SEARCH_DEBOUNCE_MS)
        return () => window.clearTimeout(timer)
    }, [searchInput])

    const filters = useMemo<LogQuery>(
        () => ({
            levels: LEVELS[prefs.level],
            search,
            excludeEventPrefixes: prefs.hideHttp ? [HTTP_EVENT_PREFIX] : [],
        }),
        [prefs.level, prefs.hideHttp, search]
    )
    const query = search.toLowerCase()

    const applyTail = useCallback((page: Awaited<ReturnType<typeof logService.getLogs>>) => {
        setEntries([...page.entries].reverse())
        cursor.current = page.cursor
        nextBefore.current = page.next_before
        setHasMore(page.has_more)
        setFile(page.file)
        setSize(page.size)
        setExpanded(new Set())
        setFresh(new Set())
        setUnseen(0)
    }, [])

    const loadTail = useCallback(async () => {
        const current = ++generation.current
        loadingTail.current = true
        olderRequest.current = null
        setLoadingOlder(false)
        // Keep the current list visible while a filter change reloads; only the first load blanks the page.
        setPhase((value) => (value === 'ready' ? value : 'loading'))
        setRefreshing(true)
        try {
            const page = await logService.getLogs({ ...filters, limit: PAGE_SIZE })
            if (current !== generation.current) return
            applyTail(page)
            setNotice('')
            setPollFailed(false)
            setPhase('ready')
            scrollRef.current?.scrollTo({ top: 0 })
        } catch (error) {
            if (current !== generation.current) return
            setLoadError(getErrorMessage(error, t('logs.loadFailed')))
            setPhase('error')
        } finally {
            if (current === generation.current) {
                loadingTail.current = false
                setRefreshing(false)
            }
        }
    }, [applyTail, filters, t])

    useEffect(() => {
        void loadTail()
        return () => {
            // Invalidate in-flight work on filter changes and unmount.
            generation.current++
            loadingTail.current = true
        }
    }, [loadTail])

    // Follow mode: poll only while visible, one request at a time, and drop
    // responses that belong to an older filter generation.
    useEffect(() => {
        if (!prefs.follow || phase !== 'ready') return
        const tick = async () => {
            if (loadingTail.current || polling.current || document.hidden) return
            polling.current = true
            const current = generation.current
            try {
                const page = await logService.getLogs({ ...filters, after: cursor.current, limit: FOLLOW_LIMIT })
                if (current !== generation.current) return
                setPollFailed(false)
                setSize(page.size)
                if (page.reset) {
                    applyTail(page)
                    setNotice(t('logs.reset'))
                    return
                }
                cursor.current = page.cursor
                if (page.gap) setNotice(t('logs.gap'))
                if (page.entries.length === 0) return
                const incoming = [...page.entries].reverse()
                const container = scrollRef.current
                if (container && !atTop.current) {
                    anchor.current = captureAnchor(container)
                    setUnseen((value) => value + incoming.length)
                }
                setFresh(new Set(incoming.map((entry) => entry.offset)))
                setBlip((value) => value + 1)
                if (page.gap) {
                    // Lines between the old list and this batch were skipped; start a fresh window.
                    nextBefore.current = incoming[incoming.length - 1].offset
                    setHasMore(nextBefore.current > 0)
                    setExpanded(new Set())
                    setEntries(incoming)
                    return
                }
                if (entriesCount.current + incoming.length > MAX_ENTRIES) trimmed.current = true
                setEntries((previous) => {
                    const offsets = new Set(incoming.map((entry) => entry.offset))
                    return [...incoming, ...previous.filter((entry) => !offsets.has(entry.offset))].slice(
                        0,
                        MAX_ENTRIES
                    )
                })
            } catch {
                if (current === generation.current) setPollFailed(true)
            } finally {
                polling.current = false
            }
        }
        const interval = window.setInterval(() => void tick(), POLL_INTERVAL_MS)
        const onVisible = () => {
            if (!document.hidden) void tick()
        }
        document.addEventListener('visibilitychange', onVisible)
        return () => {
            window.clearInterval(interval)
            document.removeEventListener('visibilitychange', onVisible)
        }
    }, [applyTail, filters, phase, prefs.follow, t])

    // Prepending rows above the viewport must not move what the reader is looking at.
    // Anchor to the row itself, not to scrollHeight: at the row cap, rows are added at the
    // top and trimmed at the bottom in the same update, so the total height barely changes.
    // Native scroll anchoring is disabled in CSS because Safari lacks it.
    useLayoutEffect(() => {
        const container = scrollRef.current
        const saved = anchor.current
        if (!container || !saved) return
        anchor.current = null
        const row = container.querySelector<HTMLElement>(`[data-offset="${saved.offset}"]`)
        if (!row) return
        const top = row.getBoundingClientRect().top - container.getBoundingClientRect().top
        container.scrollTop += top - saved.top
    }, [entries])

    const loadOlder = useCallback(async () => {
        if (
            loadingTail.current ||
            olderRequest.current !== null ||
            loadingOlder ||
            !hasMore ||
            entriesCount.current >= MAX_ENTRIES
        )
            return
        const current = generation.current
        olderRequest.current = current
        setLoadingOlder(true)
        try {
            const page = await logService.getLogs({ ...filters, before: nextBefore.current, limit: PAGE_SIZE })
            if (current !== generation.current) return
            if (page.reset) {
                applyTail(page)
                setNotice(t('logs.reset'))
                return
            }
            nextBefore.current = page.next_before
            setHasMore(page.has_more)
            const older = [...page.entries].reverse()
            if (entriesCount.current + older.length > MAX_ENTRIES) trimmed.current = true
            setEntries((previous) => {
                const offsets = new Set(previous.map((entry) => entry.offset))
                return [...previous, ...older.filter((entry) => !offsets.has(entry.offset))].slice(0, MAX_ENTRIES)
            })
        } catch (error) {
            if (current === generation.current) showToast(getErrorMessage(error, t('logs.loadFailed')), 'error')
        } finally {
            if (current === generation.current) {
                olderRequest.current = null
                setLoadingOlder(false)
            }
        }
    }, [applyTail, filters, hasMore, loadingOlder, showToast, t])

    useEffect(() => {
        const sentinel = sentinelRef.current
        const root = scrollRef.current
        if (!sentinel || !root || !hasMore || capped || refreshing || phase !== 'ready') return
        const observer = new IntersectionObserver(
            (items) => {
                if (items.some((item) => item.isIntersecting)) void loadOlder()
            },
            { root, rootMargin: '0px 0px 600px 0px' }
        )
        observer.observe(sentinel)
        return () => observer.disconnect()
    }, [capped, hasMore, loadOlder, phase, refreshing])

    const handleScroll = () => {
        const container = scrollRef.current
        if (!container) return
        const top = container.scrollTop <= TOP_THRESHOLD_PX
        atTop.current = top
        if (top && unseen > 0) setUnseen(0)
    }

    const scrollToTop = () => {
        const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
        scrollRef.current?.scrollTo({ top: 0, behavior: reduce ? 'auto' : 'smooth' })
        setUnseen(0)
    }

    const toggleExpanded = useCallback((offset: number) => {
        setExpanded((previous) => {
            const next = new Set(previous)
            if (next.has(offset)) next.delete(offset)
            else next.add(offset)
            return next
        })
    }, [])

    const copy = useCallback(
        (text: string) => {
            void copyText(text).then(
                () => showToast(t('logs.copied'), 'success'),
                () => showToast(t('logs.copyFailed'), 'error')
            )
        },
        [showToast, t]
    )

    const filterRequest = useCallback((requestId: string) => {
        setSearchInput(requestId)
        setSearch(requestId)
        setPrefs((value) => ({ ...value, level: 'all', hideHttp: false }))
    }, [])

    const download = async () => {
        if (downloading) return
        setDownloading(true)
        try {
            await logService.downloadLogFile()
        } catch (error) {
            showToast(getErrorMessage(error, t('logs.downloadFailed')), 'error')
        } finally {
            setDownloading(false)
        }
    }

    const filtered = prefs.level !== 'all' || search !== ''
    const fileName = file.split(/[\\/]/).pop() || 'cornerstone.log'
    const dateLocale = locale === 'zh' ? 'zh-CN' : 'en-US'
    const dayLabel = (date: Date) => {
        const today = new Date()
        const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1)
        if (dayKey(date) === dayKey(today)) return t('logs.today')
        if (dayKey(date) === dayKey(yesterday)) return t('logs.yesterday')
        return formatDate(date, dateLocale)
    }

    const renderList = () => {
        let lastDay = ''
        return (
            <ol className="logs-list" aria-label={t('logs.title')}>
                {entries.map((entry) => {
                    const date = parseEntryTime(entry)
                    const key = date ? dayKey(date) : lastDay
                    const separator = date && key !== lastDay
                    lastDay = key
                    return (
                        <Fragment key={entry.offset}>
                            {separator && (
                                <li className="logs-day" aria-hidden="true">
                                    <span>{dayLabel(date)}</span>
                                </li>
                            )}
                            <LogRow
                                entry={entry}
                                expanded={expanded.has(entry.offset)}
                                fresh={fresh.has(entry.offset)}
                                query={query}
                                onToggle={toggleExpanded}
                                onCopy={copy}
                                onFilterRequest={filterRequest}
                            />
                        </Fragment>
                    )
                })}
            </ol>
        )
    }

    return (
        <div className="logs-page">
            <div className="logs-page__inner">
                <PageHeader
                    title={t('logs.title')}
                    description={t('logs.description')}
                    actions={
                        <>
                            <button
                                type="button"
                                className={cx('ui-btn', 'logs-follow', prefs.follow && 'is-on')}
                                aria-pressed={prefs.follow}
                                title={t('logs.followHint')}
                                onClick={() => setPrefs((value) => ({ ...value, follow: !value.follow }))}
                            >
                                <span
                                    key={blip}
                                    className={cx('logs-live-dot', blip > 0 && 'is-blip')}
                                    aria-hidden="true"
                                />
                                {t('logs.follow')}
                            </button>
                            <button
                                type="button"
                                className="ui-btn"
                                onClick={() => void download()}
                                disabled={downloading}
                                aria-label={t('logs.download')}
                            >
                                <Icon name="download" />
                                <span className="logs-btn-label">{t('logs.download')}</span>
                            </button>
                            <button
                                type="button"
                                className="ui-icon-btn"
                                onClick={() => void loadTail()}
                                aria-label={t('logs.refresh')}
                                title={t('logs.refresh')}
                            >
                                <Icon name="refresh" />
                            </button>
                        </>
                    }
                />

                <div className="logs-toolbar">
                    <div className="logs-toolbar__search">
                        <SearchBar
                            value={searchInput}
                            onChange={setSearchInput}
                            placeholder={t('logs.searchPlaceholder')}
                        />
                    </div>
                    <div className="logs-toolbar__filters">
                        <div className="ui-segmented" role="group" aria-label={t('logs.levelFilter')}>
                            {LEVEL_FILTERS.map((level) => (
                                <button
                                    key={level}
                                    type="button"
                                    aria-pressed={prefs.level === level}
                                    onClick={() => setPrefs((value) => ({ ...value, level }))}
                                >
                                    {t(LEVEL_LABEL_KEYS[level])}
                                </button>
                            ))}
                        </div>
                        <button
                            type="button"
                            className={cx('logs-toggle-chip', prefs.hideHttp && 'is-on')}
                            aria-pressed={prefs.hideHttp}
                            onClick={() => setPrefs((value) => ({ ...value, hideHttp: !value.hideHttp }))}
                        >
                            {prefs.hideHttp && <Icon name="check" size={14} />}
                            {t('logs.hideHttp')}
                        </button>
                    </div>
                </div>

                <div className="logs-status" role="status">
                    {pollFailed && prefs.follow && (
                        <span className="logs-status__error">
                            <Icon name="alert" size={13} />
                            {t('logs.pollFailed')}
                        </span>
                    )}
                    {phase === 'ready' && <span>{t('logs.loaded', { count: entries.length })}</span>}
                    {notice && (
                        <span className="logs-status__notice">
                            <Icon name="info" size={13} />
                            {notice}
                        </span>
                    )}
                    {file && (
                        <span className="logs-status__file" title={file}>
                            <Icon name="fileText" size={13} />
                            {fileName} · {formatBytes(size)}
                        </span>
                    )}
                </div>

                {/* Fixed-height panel: the page never grows, only this list scrolls. */}
                <div className="logs-panel">
                    {unseen > 0 && (
                        <button type="button" className="logs-new-pill" onClick={scrollToTop}>
                            <Icon name="arrowUp" size={14} />
                            {t('logs.newEntries', { count: unseen })}
                        </button>
                    )}
                    <div
                        className={cx('logs-scroll', refreshing && 'is-refreshing')}
                        ref={scrollRef}
                        onScroll={handleScroll}
                        aria-busy={refreshing || phase === 'loading'}
                    >
                        {phase === 'loading' && (
                            <div className="logs-state" aria-busy="true">
                                <span className="ui-spinner" />
                            </div>
                        )}
                        {phase === 'error' && (
                            <EmptyState
                                role="alert"
                                icon="alert"
                                title={t('logs.loadFailed')}
                                description={loadError}
                                action={
                                    <button type="button" className="ui-btn" onClick={() => void loadTail()}>
                                        <Icon name="refresh" />
                                        {t('logs.retry')}
                                    </button>
                                }
                            />
                        )}
                        {phase === 'ready' && entries.length === 0 && !hasMore && (
                            <EmptyState
                                icon={filtered ? 'search' : 'terminal'}
                                title={filtered ? t('logs.emptyFiltered') : t('logs.empty')}
                                description={filtered ? t('logs.emptyFilteredHint') : t('logs.emptyHint')}
                                action={
                                    filtered ? (
                                        <button
                                            type="button"
                                            className="ui-btn"
                                            onClick={() => {
                                                setSearchInput('')
                                                setSearch('')
                                                setPrefs((value) => ({ ...value, level: 'all' }))
                                            }}
                                        >
                                            {t('logs.clearFilter')}
                                        </button>
                                    ) : undefined
                                }
                            />
                        )}
                        {phase === 'ready' && (entries.length > 0 || hasMore) && (
                            <>
                                {renderList()}
                                <div className="logs-footer" ref={sentinelRef}>
                                    {capped ? (
                                        <span className="logs-footer__capped">
                                            {t('logs.capped', { count: MAX_ENTRIES })}
                                            <button
                                                type="button"
                                                className="ui-btn ui-btn--sm ui-btn--ghost"
                                                onClick={() => void download()}
                                                disabled={downloading}
                                            >
                                                <Icon name="download" />
                                                {t('logs.download')}
                                            </button>
                                        </span>
                                    ) : hasMore ? (
                                        <button
                                            type="button"
                                            className="ui-btn ui-btn--ghost"
                                            onClick={() => void loadOlder()}
                                            disabled={loadingOlder}
                                        >
                                            {loadingOlder ? (
                                                <span className="ui-spinner" />
                                            ) : (
                                                <Icon name="chevronDown" />
                                            )}
                                            {loadingOlder ? t('logs.loadingOlder') : t('logs.loadOlder')}
                                        </button>
                                    ) : (
                                        <span>{t('logs.reachedStart')}</span>
                                    )}
                                </div>
                            </>
                        )}
                    </div>
                </div>
            </div>
        </div>
    )
}
