import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useT } from '../../contexts/I18nContext'
import { getErrorMessage, getProviders } from '../../services/api'
import { statisticsService } from '../../services/statisticsService'
import type { StatisticsSummary } from '../../types/statistics'
import { EmptyState, Group, Icon, Page, PageHeader, Row, cx } from '../ui'
import {
    USAGE_RANGES,
    formatClock,
    formatCompact,
    formatDuration,
    formatNumber,
    formatPercent,
    rangeStart,
    ratio,
    type UsageRange,
} from './usageFormat'
import './UsagePage.css'

const RANGE_KEY = 'cornerstone.usage.range'
/** Live rates cover a 60s window; 5s keeps them current without hammering the server. */
const LIVE_INTERVAL_MS = 5000
/** Bounded ranges refresh quietly. "All" scans every day file, so it only refreshes on demand. */
const REFRESH_INTERVAL_MS = 30000

type Phase = 'loading' | 'ready' | 'error'
type Realtime = StatisticsSummary['realtime']

const loadRange = (): UsageRange => {
    const saved = window.localStorage.getItem(RANGE_KEY)
    return USAGE_RANGES.includes(saved as UsageRange) ? (saved as UsageRange) : 'today'
}

const isNewer = (next: string, current: string) => !current || Date.parse(next) >= Date.parse(current)

function Metric({
    label,
    value,
    title,
    detail,
    danger,
}: {
    label: string
    value: string | null
    title?: string
    detail?: ReactNode
    danger?: boolean
}) {
    return (
        <div className="usage-metric">
            <span className="usage-metric__label">{label}</span>
            <span className={cx('usage-metric__value', value === null && 'is-placeholder')} title={title}>
                {value ?? '—'}
            </span>
            <span className={cx('usage-metric__detail', danger && 'is-danger')}>{detail}</span>
        </div>
    )
}

/** A horizontal meter. Width changes animate with transform only, from the left edge. */
function Meter({ value, className }: { value: number; className?: string }) {
    return (
        <span className={cx('usage-meter', className)} aria-hidden="true">
            <span className="usage-meter__fill" style={{ transform: `scaleX(${value})` }} />
        </span>
    )
}

export default function UsagePage() {
    const { t, locale } = useT()
    const [range, setRange] = useState<UsageRange>(loadRange)
    const [phase, setPhase] = useState<Phase>('loading')
    const [summary, setSummary] = useState<StatisticsSummary | null>(null)
    const [live, setLive] = useState<Realtime | null>(null)
    const [loadError, setLoadError] = useState<unknown>(null)
    const [refreshing, setRefreshing] = useState(false)
    const [providerNames, setProviderNames] = useState<Map<string, string>>(() => new Map())

    const generation = useRef(0)
    const loading = useRef(false)
    const livePolling = useRef(false)
    // Server clock of the newest response. Live polls derive `from` from it, so a skewed
    // browser clock can never produce a range the server rejects.
    const asOf = useRef('')

    useEffect(() => {
        window.localStorage.setItem(RANGE_KEY, range)
    }, [range])

    useEffect(() => {
        let active = true
        void getProviders().then((response) => {
            if (active && response) setProviderNames(new Map(response.providers.map((p) => [p.id, p.name])))
        })
        return () => {
            active = false
        }
    }, [])

    /** `quiet` refreshes keep the page as-is; explicit ones dim it until the answer arrives. */
    const load = useCallback(
        async (quiet = false) => {
            const current = ++generation.current
            loading.current = true
            if (!quiet) setRefreshing(true)
            try {
                const from = rangeStart(range)
                const data = await statisticsService.getStatistics(from ? { from: from.toISOString() } : {})
                if (current !== generation.current) return
                setSummary(data)
                if (isNewer(data.as_of, asOf.current)) {
                    asOf.current = data.as_of
                    setLive(data.realtime)
                }
                setLoadError(null)
                setPhase('ready')
            } catch (error) {
                if (current !== generation.current) return
                // Keep the last good figures on screen; only an empty page becomes an error page.
                setLoadError(error)
                setPhase((value) => (value === 'ready' ? value : 'error'))
            } finally {
                if (current === generation.current) {
                    loading.current = false
                    setRefreshing(false)
                }
            }
        },
        [range]
    )

    useEffect(() => {
        void load()
        return () => {
            generation.current++
        }
    }, [load])

    // Live rates: a cheap query whose history window is only the last minute.
    useEffect(() => {
        if (phase !== 'ready') return
        const tick = async () => {
            if (document.hidden || livePolling.current || !asOf.current) return
            livePolling.current = true
            try {
                const from = new Date(Date.parse(asOf.current) - 60_000).toISOString()
                const data = await statisticsService.getStatistics({ from })
                if (isNewer(data.as_of, asOf.current)) {
                    asOf.current = data.as_of
                    setLive(data.realtime)
                }
            } catch {
                // The next tick retries; live rates are best-effort.
            } finally {
                livePolling.current = false
            }
        }
        const interval = window.setInterval(() => void tick(), LIVE_INTERVAL_MS)
        const onVisible = () => {
            if (document.hidden) return
            void tick()
            if (range !== 'all' && !loading.current) void load(true)
        }
        document.addEventListener('visibilitychange', onVisible)
        return () => {
            window.clearInterval(interval)
            document.removeEventListener('visibilitychange', onVisible)
        }
    }, [load, phase, range])

    useEffect(() => {
        if (phase !== 'ready' || range === 'all') return
        const interval = window.setInterval(() => {
            if (!document.hidden && !loading.current) void load(true)
        }, REFRESH_INTERVAL_MS)
        return () => window.clearInterval(interval)
    }, [load, phase, range])

    const groups = useMemo(
        () =>
            [...(summary?.groups ?? [])].sort(
                (a, b) => b.totals.total_tokens - a.totals.total_tokens || b.totals.requests - a.totals.requests
            ),
        [summary]
    )

    const totals = summary?.totals
    const compact = (value: number) => formatCompact(value, locale)
    const exact = (value: number) => formatNumber(value, locale)
    const percent = (value: number | null) => formatPercent(value, locale)
    const duration = (ms: number) => formatDuration(ms, locale)
    // Share by tokens when any were reported; otherwise requests are the only honest measure.
    const shareByTokens = (totals?.total_tokens ?? 0) > 0

    const renderMetrics = () => {
        const requests = totals?.requests ?? 0
        const failed = totals?.failed_requests ?? 0
        return (
            <div className="usage-metrics">
                <Metric
                    label={t('usage.requests')}
                    value={totals ? compact(requests) : null}
                    title={totals ? exact(requests) : undefined}
                    danger={failed > 0}
                    detail={
                        !totals || requests === 0
                            ? null
                            : failed > 0
                              ? t('usage.failedSummary', {
                                    count: exact(failed),
                                    rate: percent(ratio(totals.successful_requests, requests)) ?? '',
                                })
                              : t('usage.allSucceeded')
                    }
                />
                <Metric
                    label={t('usage.tokens')}
                    value={totals ? compact(totals.total_tokens) : null}
                    title={totals ? exact(totals.total_tokens) : undefined}
                    detail={
                        totals && totals.total_tokens > 0
                            ? t('usage.inputOutput', {
                                  input: compact(totals.input_tokens),
                                  output: compact(totals.output_tokens),
                              })
                            : null
                    }
                />
                <Metric
                    label={t('usage.cacheHitRate')}
                    value={totals ? percent(totals.cache_hit_rate) : null}
                    detail={
                        totals && totals.cache_read_tokens > 0
                            ? t('usage.cacheHitSummary', { count: compact(totals.cache_read_tokens) })
                            : null
                    }
                />
                <Metric
                    label={t('usage.avgLatency')}
                    value={totals && requests > 0 ? duration(totals.duration_ms / requests) : null}
                    detail={
                        totals && requests > 0 ? t('usage.latencyTotal', { total: duration(totals.duration_ms) }) : null
                    }
                />
            </div>
        )
    }

    const renderModels = () => {
        if (!summary) {
            return (
                <div className="usage-placeholder">
                    <span className="ui-spinner" />
                </div>
            )
        }
        if (groups.length === 0) {
            return range === 'all' ? (
                <EmptyState icon="chart" title={t('usage.empty')} description={t('usage.emptyHint')} />
            ) : (
                <EmptyState
                    icon="chart"
                    title={t('usage.emptyRange')}
                    description={t('usage.emptyRangeHint')}
                    action={
                        <button type="button" className="ui-btn" onClick={() => setRange('all')}>
                            {t('usage.showAll')}
                        </button>
                    }
                />
            )
        }
        return groups.map(({ provider_id, model, totals: group }) => {
            const share =
                (shareByTokens
                    ? ratio(group.total_tokens, summary.totals.total_tokens)
                    : ratio(group.requests, summary.totals.requests)) ?? 0
            const tokensKnown = group.usage_reported_requests > 0
            return (
                <div key={`${provider_id}\u0000${model}`} className="ui-row usage-model">
                    <span className="ui-row__main">
                        <span className="ui-row__label usage-model__name" title={model}>
                            {model}
                        </span>
                        <span className="ui-row__detail">
                            {t('usage.modelDetail', {
                                provider: providerNames.get(provider_id) || provider_id,
                                count: exact(group.requests),
                                latency: duration(group.requests > 0 ? group.duration_ms / group.requests : 0),
                            })}
                            {group.failed_requests > 0 && (
                                <span className="usage-model__flag is-danger">
                                    {t('usage.failed', { count: exact(group.failed_requests) })}
                                </span>
                            )}
                            {group.unknown_usage_requests > 0 && (
                                <span className="usage-model__flag">
                                    {t('usage.unknownUsage', { count: exact(group.unknown_usage_requests) })}
                                </span>
                            )}
                        </span>
                    </span>
                    <span className="usage-model__figures">
                        <span
                            className="usage-model__tokens"
                            title={tokensKnown ? `${exact(group.total_tokens)} ${t('usage.tokens')}` : undefined}
                        >
                            {tokensKnown ? compact(group.total_tokens) : t('usage.noData')}
                        </span>
                        <span className="usage-model__share">{percent(share)}</span>
                    </span>
                    <Meter value={share} className="usage-model__meter" />
                </div>
            )
        })
    }

    const renderLive = () => {
        const active = (live?.rpm ?? 0) > 0
        return (
            <Group
                bare
                title={
                    <span className="usage-live__title">
                        <span className={cx('usage-live__dot', active && 'is-active')} aria-hidden="true" />
                        {t('usage.live')}
                    </span>
                }
                action={<span className="usage-live__window">{t('usage.liveWindow')}</span>}
            >
                <div className="ui-card usage-live">
                    <div className="usage-live__figure">
                        <span className="usage-live__value">{live ? compact(live.rpm) : '—'}</span>
                        <span className="usage-live__label">{t('usage.rpm')}</span>
                    </div>
                    <div className="usage-live__figure">
                        <span className="usage-live__value" title={live ? exact(live.tpm) : undefined}>
                            {live ? compact(live.tpm) : '—'}
                        </span>
                        <span className="usage-live__label">
                            {live && live.unknown_usage_requests > 0
                                ? t('usage.unknownUsage', { count: exact(live.unknown_usage_requests) })
                                : t('usage.tpm')}
                        </span>
                    </div>
                </div>
            </Group>
        )
    }

    const renderTokens = () => {
        const known = totals ? totals.input_tokens + totals.output_tokens : 0
        // A breakdown of nothing is noise, and an empty split bar would read as "all output".
        if (!totals || known === 0) return null
        const value = (count: number, extra?: string | null) => (
            <span title={exact(count)}>
                {compact(count)}
                {extra && <span className="usage-detail__extra"> · {extra}</span>}
            </span>
        )
        return (
            <Group
                title={t('usage.tokenDetail')}
                footer={
                    totals.unknown_usage_requests > 0
                        ? t('usage.coverage', { count: exact(totals.unknown_usage_requests) })
                        : t('usage.detailNote')
                }
            >
                <div className="usage-split">
                    <Meter value={ratio(totals.input_tokens, known) ?? 0} className="usage-split__meter" />
                </div>
                <Row
                    label={
                        <>
                            <span className="usage-swatch is-input" aria-hidden="true" />
                            {t('usage.input')}
                        </>
                    }
                    value={value(totals.input_tokens)}
                />
                <Row
                    className="usage-subrow"
                    label={t('usage.cacheRead')}
                    value={value(totals.cache_read_tokens, percent(totals.cache_hit_rate))}
                />
                <Row
                    className="usage-subrow"
                    label={t('usage.cacheWrite')}
                    value={value(totals.cache_creation_tokens)}
                />
                <Row
                    label={
                        <>
                            <span className="usage-swatch is-output" aria-hidden="true" />
                            {t('usage.output')}
                        </>
                    }
                    value={value(totals.output_tokens)}
                />
                <Row className="usage-subrow" label={t('usage.reasoning')} value={value(totals.reasoning_tokens)} />
            </Group>
        )
    }

    return (
        <Page wide className="usage-page">
            <PageHeader
                title={t('usage.title')}
                description={t('usage.description')}
                actions={
                    <>
                        <div className="ui-segmented" role="group" aria-label={t('usage.range')}>
                            {USAGE_RANGES.map((value) => (
                                <button
                                    key={value}
                                    type="button"
                                    aria-pressed={range === value}
                                    onClick={() => setRange(value)}
                                >
                                    {t(`usage.ranges.${value}`)}
                                </button>
                            ))}
                        </div>
                        <button
                            type="button"
                            className="ui-icon-btn"
                            onClick={() => void load()}
                            disabled={refreshing}
                            aria-label={t('usage.refresh')}
                            title={t('usage.refresh')}
                        >
                            <Icon name="refresh" className={refreshing ? 'is-spinning' : undefined} />
                        </button>
                    </>
                }
            />

            <div role="status" className="usage-notices">
                {summary && !summary.data_complete && (
                    <div className="usage-banner">
                        <Icon name="alert" />
                        <p>{t('usage.incomplete', { count: exact(summary.write_failures) })}</p>
                    </div>
                )}
                {phase === 'ready' && loadError !== null && (
                    <p className="usage-stale">
                        <Icon name="alert" size={13} />
                        {t('usage.refreshFailed')}
                    </p>
                )}
            </div>

            {phase === 'error' ? (
                <EmptyState
                    role="alert"
                    icon="alert"
                    title={t('usage.loadFailed')}
                    description={getErrorMessage(loadError, t('usage.loadFailed'))}
                    action={
                        <button type="button" className="ui-btn" onClick={() => void load()} disabled={refreshing}>
                            <Icon name="refresh" />
                            {t('usage.retry')}
                        </button>
                    }
                />
            ) : (
                <div
                    className={cx('usage-body', refreshing && summary && 'is-refreshing')}
                    aria-busy={phase === 'loading' || refreshing}
                >
                    {renderMetrics()}
                    <div className="usage-columns">
                        <Group
                            title={t('usage.byModel')}
                            className="usage-main"
                            footer={summary ? t('usage.asOf', { time: formatClock(summary.as_of, locale) }) : undefined}
                        >
                            {renderModels()}
                        </Group>
                        <aside className="usage-side">
                            {renderLive()}
                            {renderTokens()}
                        </aside>
                    </div>
                </div>
            )}
        </Page>
    )
}
