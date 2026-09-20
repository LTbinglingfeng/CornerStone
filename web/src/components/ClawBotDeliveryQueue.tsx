import { useCallback, useEffect, useRef, useState } from 'react'
import { useT } from '../contexts/I18nContext'
import { useConfirm } from '../contexts/ConfirmContext'
import { useToast } from '../contexts/ToastContext'
import { clawbotDeliveryService, type DeliveryAction, type DeliveryJob } from '../services/clawbotDeliveryService'
import './ClawBotDeliveryQueue.css'

const actions: DeliveryAction[] = ['retry', 'confirm_sent', 'cancel']

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
        <section className="clawbot-delivery" aria-labelledby="clawbot-delivery-title">
            <div className="clawbot-delivery-header">
                <h2 id="clawbot-delivery-title">{t('delivery.title')}</h2>
                <button type="button" onClick={() => void refresh()} disabled={busy || loading}>
                    {loading ? t('common.loading') : t('delivery.refresh')}
                </button>
            </div>
            <p className="clawbot-delivery-hint">{t('delivery.hint')}</p>
            <label className="clawbot-delivery-filter">
                <input
                    type="checkbox"
                    checked={showFinished}
                    onChange={(event) => setShowFinished(event.target.checked)}
                />
                {t('delivery.showFinished', { count: jobs.filter(finished).length })}
            </label>
            {error && (
                <p className="clawbot-delivery-error" role="alert">
                    {t(`delivery.${error}`)}
                </p>
            )}
            {!loading && !error && visible.length === 0 && <p>{t('delivery.empty')}</p>}
            <div className="clawbot-delivery-list">
                {visible.map((job) => (
                    <article className="clawbot-delivery-job" key={job.id}>
                        <div className="clawbot-delivery-header">
                            <strong>{t(`delivery.kind.${job.kind}`)}</strong>
                            <span className="clawbot-delivery-status">{t(`delivery.status.${job.status}`)}</span>
                        </div>
                        <div className="clawbot-delivery-meta">
                            <div>
                                {t('delivery.id')}: {job.id}
                            </div>
                            <div>
                                {t('delivery.user')}: {job.user_id}
                            </div>
                            {job.session_id && (
                                <div>
                                    {t('delivery.session')}: {job.session_id}
                                </div>
                            )}
                            {job.phase && (
                                <div>
                                    {t('delivery.phaseLabel')}: {t(`delivery.phase.${job.phase}`)}
                                </div>
                            )}
                            <div>
                                {t('delivery.created')}: {formatTime(job.created_at)}
                            </div>
                            <div>
                                {t('delivery.updated')}: {formatTime(job.updated_at)}
                            </div>
                            {job.next_attempt_at && (
                                <div>
                                    {t('delivery.nextAttempt')}: {formatTime(job.next_attempt_at)}
                                </div>
                            )}
                        </div>
                        <p>
                            {t('delivery.progress', {
                                sent: (job.chunks || []).filter((chunk) => chunk.status === 'sent').length,
                                total: (job.chunks || []).length,
                            })}
                        </p>
                        {job.last_error && <p className="clawbot-delivery-error">{job.last_error}</p>}
                        {job.status === 'uncertain' && (
                            <p className="clawbot-delivery-hint">{t('delivery.uncertainHint')}</p>
                        )}
                        <details>
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
