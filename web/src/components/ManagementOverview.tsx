import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useT } from '../contexts/I18nContext'
import { overview } from '../i18n/overview'
import { getPrompts, getProviders, getSessions } from '../services/api'
import { clawBotService } from '../services/clawbotService'
import { napCatService } from '../services/napcatService'
import { reminderService } from '../services/reminderService'
import { buildChatRoute } from '../utils/routes'
import './ManagementOverview.css'

type Resource<T> = { phase: 'loading' | 'error'; data?: never } | { phase: 'ready'; data: T }

// The existing services have no AbortSignal parameter. Ignore late results on
// refresh/unmount, and bound waiting without exposing upstream error messages.
function useOverviewResource<T>(load: () => Promise<T>, revision: number): Resource<T> {
    const [result, setResult] = useState<Resource<T>>({ phase: 'loading' })
    useEffect(() => {
        let active = true
        setResult({ phase: 'loading' })
        const timeout = window.setTimeout(() => {
            if (active) setResult({ phase: 'error' })
            active = false
        }, 20000)
        void load().then(
            (data) => {
                if (active) setResult({ phase: 'ready', data })
                window.clearTimeout(timeout)
            },
            () => {
                if (active) setResult({ phase: 'error' })
                window.clearTimeout(timeout)
            }
        )
        return () => {
            active = false
            window.clearTimeout(timeout)
        }
    }, [load, revision])
    return result
}

const loadPersonaCount = async () => (await getPrompts()).length
const loadReminderCount = async () =>
    (await reminderService.listReminders()).filter((r) => r.status === 'pending').length
const loadModel = async () => {
    const response = await getProviders()
    if (!response) throw new Error('Unavailable')
    const provider = response.providers.find((item) => item.id === response.active_provider_id)
    return provider ? { name: provider.name, model: provider.model } : null
}
const loadWechat = async () => {
    const settings = await clawBotService.getSettings()
    return { status: settings.status, polling: settings.polling }
}
const loadQQ = async () => {
    const settings = await napCatService.getSettings()
    return { status: settings.status }
}
const timestamp = (value: string) => {
    const time = Date.parse(value)
    return Number.isFinite(time) ? time : 0
}

export default function ManagementOverview() {
    const { locale } = useT()
    const copy = overview[locale]
    const [revision, setRevision] = useState(0)
    const sessions = useOverviewResource(getSessions, revision)
    const personas = useOverviewResource(loadPersonaCount, revision)
    const reminders = useOverviewResource(loadReminderCount, revision)
    const model = useOverviewResource(loadModel, revision)
    const wechat = useOverviewResource(loadWechat, revision)
    const qq = useOverviewResource(loadQQ, revision)
    const resources = [sessions, personas, reminders, model, wechat, qq]
    const loading = resources.some((item) => item.phase === 'loading')
    const failed = resources.some((item) => item.phase === 'error')
    const refresh = () => setRevision((value) => value + 1)
    const placeholder = (phase: Resource<unknown>['phase']) => (phase === 'loading' ? copy.loading : copy.unavailable)
    const statusLabel = (status: string) =>
        Object.prototype.hasOwnProperty.call(copy.statuses, status)
            ? copy.statuses[status as keyof typeof copy.statuses]
            : copy.statuses.unknown
    const recent =
        sessions.phase === 'ready'
            ? [...sessions.data].sort((a, b) => timestamp(b.updated_at) - timestamp(a.updated_at)).slice(0, 5)
            : []
    const dateFormatter = new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en', {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
    })

    return (
        <div className="management-overview">
            <div className="overview-content">
                <header className="overview-heading">
                    <div>
                        <h1>{copy.title}</h1>
                        <p>{copy.subtitle}</p>
                    </div>
                    <button type="button" className="overview-button" onClick={refresh} disabled={loading}>
                        {loading ? copy.loading : failed ? copy.retry : copy.refresh}
                    </button>
                </header>
                <div role="status" className="overview-feedback">
                    {failed && <p>{copy.partialFailure}</p>}
                </div>

                <section aria-labelledby="overview-channels">
                    <div className="overview-section-heading">
                        <h2 id="overview-channels">{copy.channels}</h2>
                        <Link to="/channels">
                            {copy.allChannels}
                            <span aria-hidden="true"> ↗</span>
                        </Link>
                    </div>
                    <div className="overview-channels">
                        {(
                            [
                                {
                                    key: 'wechat',
                                    title: copy.wechat,
                                    hint: copy.wechatHint,
                                    action: copy.manageWechat,
                                    resource: wechat,
                                },
                                { key: 'qq', title: copy.qq, hint: copy.qqHint, action: copy.manageQQ, resource: qq },
                            ] as const
                        ).map(({ key, title, hint, action, resource }) => (
                            <article
                                key={key}
                                className={`overview-channel overview-channel-${key}`}
                                aria-busy={resource.phase === 'loading'}
                            >
                                <div className="overview-channel-top">
                                    <h3>{title}</h3>
                                    <span
                                        className={`overview-status ${resource.phase === 'ready' && ['running', 'connected'].includes(resource.data.status) ? 'is-running' : ''}`}
                                    >
                                        <span aria-hidden="true" className="overview-status-dot" />
                                        {resource.phase === 'ready'
                                            ? statusLabel(resource.data.status)
                                            : placeholder(resource.phase)}
                                    </span>
                                </div>
                                <p>{hint}</p>
                                {key === 'wechat' && wechat.phase === 'ready' && (
                                    <p className="overview-polling">
                                        {wechat.data.polling ? copy.polling : copy.notPolling}
                                    </p>
                                )}
                                <Link
                                    className="overview-button overview-channel-action"
                                    to={`/channels?channel=${key}`}
                                >
                                    {action}
                                    <span aria-hidden="true">→</span>
                                </Link>
                            </article>
                        ))}
                    </div>
                </section>

                <div className="overview-stats">
                    {[
                        {
                            label: copy.personas,
                            value: personas.phase === 'ready' ? personas.data : placeholder(personas.phase),
                            to: '/contacts',
                        },
                        {
                            label: copy.sessions,
                            value: sessions.phase === 'ready' ? sessions.data.length : placeholder(sessions.phase),
                            to: '/chat',
                        },
                        {
                            label: copy.reminders,
                            value: reminders.phase === 'ready' ? reminders.data : placeholder(reminders.phase),
                            to: '/settings?section=automation',
                        },
                    ].map(({ label, value, to }) => (
                        <Link className="overview-stat" key={to} to={to}>
                            <span>{label}</span>
                            <strong className={typeof value === 'number' ? '' : 'overview-stat-placeholder'}>
                                {value}
                            </strong>
                        </Link>
                    ))}
                    <Link className="overview-stat overview-model" to="/settings?section=models&panel=providers">
                        <span>{copy.model}</span>
                        <strong>
                            {model.phase === 'ready'
                                ? model.data?.model || copy.unconfigured
                                : placeholder(model.phase)}
                        </strong>
                        {model.phase === 'ready' && model.data && <small>{model.data.name}</small>}
                    </Link>
                </div>

                <div className="overview-bottom">
                    <section
                        className="overview-recent"
                        aria-labelledby="overview-recent"
                        aria-busy={sessions.phase === 'loading'}
                    >
                        <div className="overview-section-heading">
                            <h2 id="overview-recent">{copy.recent}</h2>
                            <Link to="/chat">
                                {copy.allSessions}
                                <span aria-hidden="true"> ↗</span>
                            </Link>
                        </div>
                        {sessions.phase !== 'ready' ? (
                            <div className="overview-empty">
                                <p>{placeholder(sessions.phase)}</p>
                            </div>
                        ) : recent.length === 0 ? (
                            <div className="overview-empty">
                                <h3>{copy.empty}</h3>
                                <p>{copy.emptyHint}</p>
                                <Link className="overview-button" to="/chat">
                                    {copy.openChat}
                                </Link>
                            </div>
                        ) : (
                            <ul className="overview-session-list">
                                {recent.map((session) => (
                                    <li key={session.id}>
                                        <Link to={buildChatRoute(session.id, session.prompt_id)}>
                                            <div className="overview-session-text">
                                                <strong>{session.title || copy.untitled}</strong>
                                                {session.prompt_name && <span>{session.prompt_name}</span>}
                                            </div>
                                            {timestamp(session.updated_at) !== 0 && (
                                                <time dateTime={session.updated_at} title={copy.updated}>
                                                    {dateFormatter.format(new Date(session.updated_at))}
                                                </time>
                                            )}
                                            <span aria-hidden="true">↗</span>
                                        </Link>
                                    </li>
                                ))}
                            </ul>
                        )}
                    </section>
                    <section className="overview-configuration" aria-labelledby="overview-configuration">
                        <div className="overview-section-heading">
                            <h2 id="overview-configuration">{copy.configuration}</h2>
                        </div>
                        {[
                            {
                                title: copy.providers,
                                hint: copy.providersHint,
                                to: '/settings?section=models&panel=providers',
                            },
                            { title: copy.automation, hint: copy.automationHint, to: '/settings?section=automation' },
                            { title: copy.memory, hint: copy.memoryHint, to: '/settings?section=memory' },
                        ].map(({ title, hint, to }) => (
                            <Link className="overview-config-link" key={to} to={to}>
                                <div>
                                    <strong>{title}</strong>
                                    <p>{hint}</p>
                                </div>
                                <span aria-hidden="true">↗</span>
                            </Link>
                        ))}
                    </section>
                </div>
            </div>
        </div>
    )
}
