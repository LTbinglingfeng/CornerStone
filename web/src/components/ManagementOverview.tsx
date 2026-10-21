import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useManagementResource, type ManagementResource } from '../hooks/useManagementResource'
import { useT } from '../contexts/I18nContext'
import { overview } from '../i18n/overview'
import { appendQueryParam, getPromptAvatarUrl, getPrompts, getProviders, getSessions } from '../services/api'
import { clawBotService } from '../services/clawbotService'
import { napCatService } from '../services/napcatService'
import { reminderService } from '../services/reminderService'
import { buildChatRoute } from '../utils/routes'
import { formatRelativeTime } from '../utils/time'
import { Avatar, Badge, EmptyState, Group, Icon, Page, PageHeader, Row } from './ui'
import { channelStatusTone } from './channelStatus'
import './ManagementOverview.css'

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
    return { status: settings.status, polling: settings.polling, persona: settings.prompt_name || '' }
}
const loadQQ = async () => {
    const settings = await napCatService.getSettings()
    return { status: settings.status, persona: settings.prompt_name || '' }
}
const timestamp = (value: string) => {
    const time = Date.parse(value)
    return Number.isFinite(time) ? time : 0
}

export default function ManagementOverview() {
    const { locale } = useT()
    const copy = overview[locale]
    const [revision, setRevision] = useState(0)
    const sessions = useManagementResource(getSessions, revision)
    const personas = useManagementResource(getPrompts, revision)
    const reminders = useManagementResource(loadReminderCount, revision)
    const model = useManagementResource(loadModel, revision)
    const wechat = useManagementResource(loadWechat, revision)
    const qq = useManagementResource(loadQQ, revision)
    const resources = [sessions, personas, reminders, model, wechat, qq]
    const loading = resources.some((item) => item.phase === 'loading')
    const failed = resources.some((item) => item.phase === 'error')
    const refresh = () => setRevision((value) => value + 1)
    const placeholder = (phase: ManagementResource<unknown>['phase']) =>
        phase === 'loading' ? copy.loading : copy.unavailable
    const statusLabel = (status: string) =>
        Object.prototype.hasOwnProperty.call(copy.statuses, status)
            ? copy.statuses[status as keyof typeof copy.statuses]
            : copy.statuses.unknown

    const personaById = useMemo(
        () => new Map(personas.phase === 'ready' ? personas.data.map((p) => [p.id, p]) : []),
        [personas]
    )
    const recent =
        sessions.phase === 'ready'
            ? [...sessions.data].sort((a, b) => timestamp(b.updated_at) - timestamp(a.updated_at)).slice(0, 5)
            : []

    const metric = (phase: ManagementResource<unknown>['phase'], value: number | undefined) =>
        phase === 'ready' ? (
            <span className="overview-metric__value">{value}</span>
        ) : (
            <span className="overview-metric__value is-placeholder">
                {phase === 'loading' ? '—' : copy.unavailable}
            </span>
        )

    const channels = [
        { key: 'wechat', title: copy.wechat, icon: 'message', resource: wechat },
        { key: 'qq', title: copy.qq, icon: 'chat', resource: qq },
    ] as const

    return (
        <Page wide className="management-overview">
            <PageHeader
                title={copy.title}
                description={copy.subtitle}
                actions={
                    <button
                        type="button"
                        className="ui-btn overview-refresh"
                        onClick={refresh}
                        disabled={loading}
                        aria-busy={loading}
                    >
                        <Icon name="refresh" className={loading ? 'is-spinning' : undefined} />
                        {copy.refresh}
                    </button>
                }
            />

            <div role="status" className="overview-feedback">
                {failed && (
                    <div className="overview-banner">
                        <Icon name="alert" />
                        <p>{copy.partialFailure}</p>
                        <button type="button" className="ui-btn ui-btn--sm" onClick={refresh} disabled={loading}>
                            {copy.retry}
                        </button>
                    </div>
                )}
            </div>

            <div className="overview-metrics" aria-busy={loading}>
                <Link className="overview-metric" to="/contacts">
                    <span className="overview-metric__label">{copy.personas}</span>
                    {metric(personas.phase, personas.phase === 'ready' ? personas.data.length : undefined)}
                </Link>
                <Link className="overview-metric" to="/chat">
                    <span className="overview-metric__label">{copy.sessions}</span>
                    {metric(sessions.phase, sessions.phase === 'ready' ? sessions.data.length : undefined)}
                </Link>
                <Link className="overview-metric" to="/settings?section=automation">
                    <span className="overview-metric__label">{copy.reminders}</span>
                    {metric(reminders.phase, reminders.phase === 'ready' ? reminders.data : undefined)}
                </Link>
                <Link className="overview-metric overview-metric--model" to="/settings?section=models&panel=providers">
                    <span className="overview-metric__label">
                        {copy.model}
                        {model.phase === 'ready' && model.data?.name && (
                            <span className="overview-metric__provider">{model.data.name}</span>
                        )}
                    </span>
                    {model.phase === 'ready' ? (
                        <span className={`overview-metric__value${model.data?.model ? '' : ' is-placeholder'}`}>
                            {model.data?.model || copy.unconfigured}
                        </span>
                    ) : (
                        <span className="overview-metric__value is-placeholder">
                            {model.phase === 'loading' ? '—' : copy.unavailable}
                        </span>
                    )}
                </Link>
            </div>

            <div className="overview-columns">
                <div className="overview-main">
                    <Group title={copy.channels}>
                        {channels.map(({ key, title, icon, resource }) => (
                            <Row
                                key={key}
                                icon={icon}
                                label={title}
                                to={`/channels?channel=${key}`}
                                detail={
                                    resource.phase === 'ready'
                                        ? resource.data.persona
                                            ? `${copy.personaPrefix} · ${resource.data.persona}`
                                            : copy.noPersona
                                        : undefined
                                }
                                trailing={
                                    resource.phase === 'ready' ? (
                                        <Badge tone={channelStatusTone(resource.data.status)} dot>
                                            {statusLabel(resource.data.status)}
                                        </Badge>
                                    ) : (
                                        <Badge>{placeholder(resource.phase)}</Badge>
                                    )
                                }
                            />
                        ))}
                    </Group>

                    <Group
                        title={copy.recent}
                        action={
                            <Link className="overview-link" to="/chat">
                                {copy.allSessions}
                                <Icon name="chevronRight" size={14} />
                            </Link>
                        }
                    >
                        <div aria-busy={sessions.phase === 'loading'}>
                            {sessions.phase !== 'ready' ? (
                                <p className="overview-placeholder">{placeholder(sessions.phase)}</p>
                            ) : recent.length === 0 ? (
                                <EmptyState
                                    icon="chat"
                                    title={copy.empty}
                                    description={copy.emptyHint}
                                    action={
                                        <Link className="ui-btn" to="/chat">
                                            {copy.openChat}
                                        </Link>
                                    }
                                />
                            ) : (
                                recent.map((session) => {
                                    const persona = session.prompt_id ? personaById.get(session.prompt_id) : undefined
                                    const personaName = persona?.name || session.prompt_name || ''
                                    const avatar = persona?.avatar
                                        ? appendQueryParam(
                                              getPromptAvatarUrl(persona.id),
                                              't',
                                              new Date(persona.updated_at).getTime()
                                          )
                                        : null
                                    return (
                                        <Link
                                            key={session.id}
                                            className="ui-row overview-session"
                                            to={buildChatRoute(session.id, session.prompt_id)}
                                        >
                                            <Avatar src={avatar} name={personaName || session.title || '?'} />
                                            <span className="ui-row__main">
                                                <span className="ui-row__label overview-session__title">
                                                    {session.title || copy.untitled}
                                                </span>
                                                {personaName && <span className="ui-row__detail">{personaName}</span>}
                                            </span>
                                            {timestamp(session.updated_at) !== 0 && (
                                                <time
                                                    className="overview-session__time"
                                                    dateTime={session.updated_at}
                                                    title={`${copy.updated} ${new Date(session.updated_at).toLocaleString(locale === 'zh' ? 'zh-CN' : 'en')}`}
                                                >
                                                    {formatRelativeTime(session.updated_at, locale)}
                                                </time>
                                            )}
                                        </Link>
                                    )
                                })
                            )}
                        </div>
                    </Group>
                </div>

                <aside className="overview-side">
                    <Group title={copy.configuration}>
                        <Row
                            icon="sparkles"
                            label={copy.providers}
                            detail={copy.providersHint}
                            to="/settings?section=models&panel=providers"
                        />
                        <Row
                            icon="clock"
                            label={copy.automation}
                            detail={copy.automationHint}
                            to="/settings?section=automation"
                        />
                        <Row icon="memory" label={copy.memory} detail={copy.memoryHint} to="/settings?section=memory" />
                    </Group>
                </aside>
            </div>
        </Page>
    )
}
