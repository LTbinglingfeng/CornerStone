import { useCallback, useEffect, useRef, useState } from 'react'
import { useT } from '../contexts/I18nContext'
import { useConfirm } from '../contexts/ConfirmContext'
import { useToast } from '../contexts/ToastContext'
import { clawbotDeliveryService, type DeliveryAction, type DeliveryJob } from '../services/clawbotDeliveryService'
import { Badge, Toggle, type Tone } from './ui'
import './ClawBotDeliveryQueue.css'

const actions: DeliveryAction[] = ['retry', 'confirm_sent', 'cancel']

const jobTone = (status: DeliveryJob['status']): Tone => {
    if (status === 'done') return 'success'
    if (status === 'uncertain' || status === 'blocked') return 'warning'
    if (status === 'cancelled') return 'neutral'
    return 'info'
}

export default function ClawBotDeliveryQueue() {
    const { t, locale } = useT()
    const { confirm } = useConfirm()
    const { showToast } = useToast()
    const [jobs, setJobs] = useState<DeliveryJob[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<'loadFailed' | 'actionFailed' | null>(null)
    const [busy, setBusy] = useState(false)
    const [showFinished, setShowFinished] = useState(false)
    const mounted = useRef(false)
    const generation = useRef(0)
    const controller = useRef<AbortController | null>(null)
    const actionPending = useRef(false)
    const fetching = useRef(false)

    const refresh = useCallback(async () => {
        if (!mounted.current || actionPending.current) return
        controller.current?.abort()
        const request = new AbortController()
        controller.current = request
        const version = ++generation.current
        const active = () => mounted.current && !request.signal.aborted && version === generation.current
        setLoading(true)
        fetching.current = true
        try {
            const next = await clawbotDeliveryService.list(request.signal)
            if (active()) {
                setJobs(next)
                setError(null)
            }
        } catch {
            if (active()) setError('loadFailed')
        } finally {
            if (active()) {
                fetching.current = false
                setLoading(false)
            }
        }
    }, [])

    useEffect(() => {
        mounted.current = true
        void refresh()
        const timer = window.setInterval(() => {
            if (!controller.current || controller.current.signal.aborted) return
            // Do not interrupt a slow request or an action/confirmation.
            if (!actionPending.current && !fetching.current) void refresh()
        }, 10000)
        return () => {
            mounted.current = false
            generation.current += 1
            controller.current?.abort()
            window.clearInterval(timer)
        }
    }, [refresh])

    const act = async (job: DeliveryJob, action: DeliveryAction) => {
        if (actionPending.current || !job.allowed_actions.includes(action)) return
        if (action === 'confirm_sent' && job.phase !== 'send') return
        actionPending.current = true
        setBusy(true)
        controller.current?.abort()
        const version = ++generation.current
        const request = new AbortController()
        controller.current = request
        const active = () => mounted.current && !request.signal.aborted && generation.current === version
        try {
            const accepted = await confirm({
                title: t(`delivery.actions.${action}`),
                message: t(`delivery.confirm.${action}`),
                confirmText: t(`delivery.actions.${action}`),
                danger: action !== 'confirm_sent',
            })
            if (!active() || !accepted) return
            const next = await clawbotDeliveryService.act(job, action, request.signal)
            if (!active()) return
            setJobs(next)
            setError(null)
            showToast(t('delivery.actionSuccess'), 'success')
        } catch {
            if (active()) {
                setError('actionFailed')
                showToast(t('delivery.actionFailed'), 'error')
            }
        } finally {
            actionPending.current = false
            if (active()) {
                setBusy(false)
                fetching.current = false
                setLoading(false)
                // Never automatically replay a POST, even if its response was lost.
            }
        }
    }

    const formatTime = (value: string) => {
        const date = new Date(value)
        return Number.isNaN(date.getTime()) ? value : date.toLocaleString(locale === 'zh' ? 'zh-CN' : 'en-US')
    }
    const finished = (job: DeliveryJob) => job.status === 'done' || (job.status === 'cancelled' && !job.history_pending)
    const visible = jobs.filter((job) => showFinished || !finished(job))

    return (
        <section className="ui-group clawbot-delivery" aria-labelledby="clawbot-delivery-title">
            <div className="ui-group__head">
                <div>
                    <h2 className="ui-group__title" id="clawbot-delivery-title">
                        {t('delivery.title')}
                    </h2>
                    <p className="ui-group__desc">{t('delivery.hint')}</p>
                </div>
                <button
                    type="button"
                    className="ui-btn ui-btn--sm"
                    onClick={() => void refresh()}
                    disabled={busy || loading}
                >
                    {loading ? t('common.loading') : t('delivery.refresh')}
                </button>
            </div>
            <div className="ui-list clawbot-delivery-list">
                <div className="ui-row">
                    <span className="ui-row__main">
                        <span className="ui-row__label">
                            {t('delivery.showFinished', { count: jobs.filter(finished).length })}
                        </span>
                    </span>
                    <Toggle
                        label={t('delivery.showFinished', { count: jobs.filter(finished).length })}
                        checked={showFinished}
                        onChange={setShowFinished}
                    />
                </div>
                {error && (
                    <p className="clawbot-delivery-error clawbot-delivery-block" role="alert">
                        {t(`delivery.${error}`)}
                    </p>
                )}
                {!loading && !error && visible.length === 0 && (
                    <p className="clawbot-delivery-empty clawbot-delivery-block">{t('delivery.empty')}</p>
                )}
                {visible.map((job) => (
                    <article className="clawbot-delivery-job clawbot-delivery-block" key={job.id}>
                        <div className="clawbot-delivery-job__head">
                            <strong>{t(`delivery.kind.${job.kind}`)}</strong>
                            <Badge tone={jobTone(job.status)} dot>
                                {t(`delivery.status.${job.status}`)}
                            </Badge>
                        </div>
                        <dl className="clawbot-delivery-meta">
                            <div>
                                <dt>{t('delivery.id')}</dt>
                                <dd>{job.id}</dd>
                            </div>
                            <div>
                                <dt>{t('delivery.user')}</dt>
                                <dd>{job.user_id}</dd>
                            </div>
                            {job.session_id && (
                                <div>
                                    <dt>{t('delivery.session')}</dt>
                                    <dd>{job.session_id}</dd>
                                </div>
                            )}
                            {job.phase && (
                                <div>
                                    <dt>{t('delivery.phaseLabel')}</dt>
                                    <dd>{t(`delivery.phase.${job.phase}`)}</dd>
                                </div>
                            )}
                            <div>
                                <dt>{t('delivery.created')}</dt>
                                <dd>{formatTime(job.created_at)}</dd>
                            </div>
                            <div>
                                <dt>{t('delivery.updated')}</dt>
                                <dd>{formatTime(job.updated_at)}</dd>
                            </div>
                            {job.next_attempt_at && (
                                <div>
                                    <dt>{t('delivery.nextAttempt')}</dt>
                                    <dd>{formatTime(job.next_attempt_at)}</dd>
                                </div>
                            )}
                        </dl>
                        <p className="clawbot-delivery-progress">
                            {t('delivery.progress', {
                                sent: (job.chunks || []).filter((chunk) => chunk.status === 'sent').length,
                                total: (job.chunks || []).length,
                            })}
                        </p>
                        {job.last_error && <p className="clawbot-delivery-error">{job.last_error}</p>}
                        {job.status === 'uncertain' && (
                            <p className="clawbot-delivery-warning">{t('delivery.uncertainHint')}</p>
                        )}
                        <details className="clawbot-delivery-chunks">
                            <summary>{t('delivery.chunks')}</summary>
                            {(job.chunks || []).map((chunk, index) => (
                                <div className="clawbot-delivery-chunk" key={index}>
                                    <strong>
                                        {t('delivery.chunk', { number: index + 1 })} ·{' '}
                                        {t(`delivery.chunkStatus.${chunk.status}`)}
                                    </strong>
                                    {chunk.media ? (
                                        <p>
                                            {t(`delivery.media.${chunk.media.kind}`)} · {chunk.media.name} ·{' '}
                                            {chunk.media.size.toLocaleString()} B
                                            <br />
                                            {t(`delivery.upload.${chunk.media.upload_status}`)}
                                        </p>
                                    ) : (
                                        <pre>{chunk.text}</pre>
                                    )}
                                    {chunk.message_id && <p>{t('delivery.messageId', { id: chunk.message_id })}</p>}
                                    {chunk.manual_confirmation && <p>{t('delivery.manuallyConfirmed')}</p>}
                                </div>
                            ))}
                        </details>
                        <div className="clawbot-delivery-actions">
                            {actions
                                .filter(
                                    (action) =>
                                        job.allowed_actions.includes(action) &&
                                        (action !== 'confirm_sent' || job.phase === 'send')
                                )
                                .map((action) => (
                                    <button
                                        type="button"
                                        key={action}
                                        className={`ui-btn ui-btn--sm${action === 'cancel' ? ' ui-btn--danger' : ''}`}
                                        disabled={busy || loading || !!error}
                                        onClick={() => void act(job, action)}
                                    >
                                        {t(`delivery.actions.${action}`)}
                                    </button>
                                ))}
                        </div>
                    </article>
                ))}
            </div>
        </section>
    )
}
