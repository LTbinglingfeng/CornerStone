import { useEffect, useMemo, useRef, useState } from 'react'
import type { ChatSession, Prompt } from '../types/chat'
import {
    appendQueryParam,
    deleteSession,
    getErrorMessage,
    getPromptAvatarUrl,
    getPrompts,
    getSessions,
} from '../services/api'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import { getManagementCopy } from '../i18n/archive'
import type { Locale } from '../i18n'
import { Avatar, EmptyState, Icon } from './ui'
import './ChatList.css'

interface ChatListProps {
    onSelectSession: (id: string, promptId?: string) => void
    searchQuery?: string
    refreshToken?: number
    activeSessionId?: string | null
}

interface ArchivedSession {
    session: ChatSession
    prompt?: Prompt
    personaName: string
}

const NO_PERSONA_FILTER = '__none__'
const DAY_MS = 24 * 60 * 60 * 1000

const startOfDay = (date: Date) => new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()

/** Compact, list-friendly timestamp: today → time, yesterday, this week → weekday, otherwise a date. */
function formatListTime(value: string, locale: Locale, now = new Date()) {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return ''
    const tag = locale === 'en' ? 'en-US' : 'zh-CN'
    const days = Math.round((startOfDay(now) - startOfDay(date)) / DAY_MS)
    if (days <= 0) {
        return new Intl.DateTimeFormat(tag, { hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
    }
    if (days === 1) {
        const label = new Intl.RelativeTimeFormat(tag, { numeric: 'auto' }).format(-1, 'day')
        return label.charAt(0).toLocaleUpperCase(tag) + label.slice(1)
    }
    if (days < 7) return new Intl.DateTimeFormat(tag, { weekday: 'short' }).format(date)
    if (date.getFullYear() === now.getFullYear()) {
        return new Intl.DateTimeFormat(tag, { month: 'short', day: 'numeric' }).format(date)
    }
    return new Intl.DateTimeFormat(tag, { year: 'numeric', month: 'numeric', day: 'numeric' }).format(date)
}

const ChatList: React.FC<ChatListProps> = ({ onSelectSession, searchQuery = '', refreshToken, activeSessionId }) => {
    const { locale, t } = useT()
    const copy = getManagementCopy(locale).archive
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [sessions, setSessions] = useState<ArchivedSession[]>([])
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [personaFilter, setPersonaFilter] = useState('')
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [deletingId, setDeletingId] = useState('')
    const requestRef = useRef(0)
    const lastRefreshTokenRef = useRef<number | undefined>(undefined)

    const loadData = async (showLoading = true) => {
        const request = ++requestRef.current
        if (showLoading) setLoading(true)
        try {
            const [sessionData, promptData] = await Promise.all([getSessions(), getPrompts()])
            if (request !== requestRef.current) return
            const promptMap = new Map(promptData.map((prompt) => [prompt.id, prompt]))
            const archive = sessionData
                .map((session) => {
                    const prompt = session.prompt_id ? promptMap.get(session.prompt_id) : undefined
                    return {
                        session,
                        prompt,
                        personaName: prompt?.name || session.prompt_name || copy.noPersona,
                    }
                })
                .sort((a, b) => new Date(b.session.updated_at).getTime() - new Date(a.session.updated_at).getTime())

            setPrompts(promptData)
            setSessions(archive)
            setError('')
        } catch (loadError) {
            if (request !== requestRef.current) return
            setError(getErrorMessage(loadError, copy.loadError))
        } finally {
            if (request === requestRef.current) setLoading(false)
        }
    }

    useEffect(() => {
        if (typeof refreshToken === 'number') lastRefreshTokenRef.current = refreshToken
        loadData()
    }, [])

    useEffect(() => {
        if (typeof refreshToken !== 'number' || lastRefreshTokenRef.current === refreshToken) return
        lastRefreshTokenRef.current = refreshToken
        loadData(false)
    }, [refreshToken])

    const filteredSessions = useMemo(() => {
        const query = searchQuery.trim().toLocaleLowerCase(locale)
        return sessions.filter(({ session, personaName }) => {
            const matchesQuery = !query || `${session.title} ${personaName}`.toLocaleLowerCase(locale).includes(query)
            const matchesPersona =
                !personaFilter ||
                (personaFilter === NO_PERSONA_FILTER
                    ? !session.prompt_id || !prompts.some((prompt) => prompt.id === session.prompt_id)
                    : session.prompt_id === personaFilter)
            return matchesQuery && matchesPersona
        })
    }, [locale, personaFilter, prompts, searchQuery, sessions])

    const getAvatarUrl = (prompt?: Prompt) => {
        if (!prompt?.avatar) return null
        return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
    }

    const handleDelete = async (item: ArchivedSession) => {
        const accepted = await confirm({
            title: copy.deleteTitle,
            message: copy.deleteMessage(item.session.title),
            confirmText: copy.delete,
            danger: true,
        })
        if (!accepted) return

        setDeletingId(item.session.id)
        try {
            const success = await deleteSession(item.session.id)
            if (!success) throw new Error(copy.deleteFailed)
            setSessions((current) => current.filter(({ session }) => session.id !== item.session.id))
        } catch (deleteError) {
            showToast(getErrorMessage(deleteError, copy.deleteFailed), 'error')
        } finally {
            setDeletingId('')
        }
    }

    const hasFilters = Boolean(searchQuery.trim() || personaFilter)
    const now = new Date()

    return (
        <section className="archive" aria-label={copy.conversations}>
            <div className="archive-toolbar">
                <label className="archive-filter">
                    <span className="ui-visually-hidden">{copy.filterLabel}</span>
                    <select
                        className="ui-select"
                        value={personaFilter}
                        onChange={(event) => setPersonaFilter(event.target.value)}
                    >
                        <option value="">{copy.allPersonas}</option>
                        {prompts.map((prompt) => (
                            <option key={prompt.id} value={prompt.id}>
                                {prompt.name}
                            </option>
                        ))}
                        {personaFilter &&
                            personaFilter !== NO_PERSONA_FILTER &&
                            !prompts.some((prompt) => prompt.id === personaFilter) && (
                                <option value={personaFilter}>{copy.deletedPersona}</option>
                            )}
                        <option value={NO_PERSONA_FILTER}>{copy.noPersona}</option>
                    </select>
                </label>
                <p className="ui-visually-hidden" aria-live="polite">
                    {loading ? '' : copy.results(filteredSessions.length, sessions.length)}
                </p>
            </div>

            {error && sessions.length > 0 && (
                <div className="archive-error" role="alert">
                    <span>{error}</span>
                    <button type="button" className="ui-btn ui-btn--sm" disabled={loading} onClick={() => loadData()}>
                        {copy.retry}
                    </button>
                </div>
            )}
            {loading && sessions.length === 0 ? (
                <div className="archive-state" role="status">
                    <span className="ui-spinner" aria-hidden="true" />
                    <span className="ui-visually-hidden">{t('common.loading')}</span>
                </div>
            ) : error && sessions.length === 0 ? (
                <EmptyState
                    icon="alert"
                    role="alert"
                    title={error}
                    action={
                        <button type="button" className="ui-btn ui-btn--sm" onClick={() => loadData()}>
                            {copy.retry}
                        </button>
                    }
                />
            ) : filteredSessions.length === 0 ? (
                hasFilters ? (
                    <EmptyState icon="search" title={copy.noResults} description={copy.noResultsHint} />
                ) : (
                    <EmptyState icon="chat" title={copy.empty} description={copy.emptyHint} />
                )
            ) : (
                <ul className="archive-list">
                    {filteredSessions.map((item) => {
                        const active = item.session.id === activeSessionId
                        const time = formatListTime(item.session.updated_at, locale, now)
                        return (
                            <li key={item.session.id} className={`archive-card${active ? ' is-active' : ''}`}>
                                <button
                                    type="button"
                                    className="archive-open"
                                    aria-current={active ? 'true' : undefined}
                                    onClick={() => onSelectSession(item.session.id, item.session.prompt_id)}
                                >
                                    <Avatar src={getAvatarUrl(item.prompt)} name={item.personaName} />
                                    <span className="archive-main">
                                        <span className="archive-title">{item.session.title}</span>
                                        <span className="archive-meta">
                                            <span className="archive-persona">{item.personaName}</span>
                                            {time && (
                                                <>
                                                    <span aria-hidden="true"> · </span>
                                                    <time dateTime={item.session.updated_at}>{time}</time>
                                                </>
                                            )}
                                        </span>
                                    </span>
                                </button>
                                <button
                                    type="button"
                                    className="archive-delete ui-icon-btn"
                                    disabled={Boolean(deletingId)}
                                    onClick={() => handleDelete(item)}
                                    aria-label={`${copy.delete}: ${item.session.title}`}
                                    title={copy.delete}
                                >
                                    <Icon name="trash" size={16} />
                                </button>
                            </li>
                        )
                    })}
                </ul>
            )}
        </section>
    )
}

export default ChatList
