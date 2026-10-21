import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useT } from '../contexts/I18nContext'
import { useManagementResource } from '../hooks/useManagementResource'
import { getChannelsCopy } from '../i18n/channels'
import { clawBotService } from '../services/clawbotService'
import { napCatService } from '../services/napcatService'
import ClawBotSettingsPanel from './ClawBotSettings'
import NapCatSettingsPanel from './NapCatSettings'
import QQOfficialSettingsPanel from './QQOfficialSettings'
import { qqOfficialService } from '../services/qqOfficialService'
import { Badge, Icon, Page, PageHeader } from './ui'
import { channelStatusTone } from './channelStatus'
import './ChannelsPage.css'

export default function ChannelsPage() {
    const [params, setParams] = useSearchParams()
    const channel = params.get('channel')
    if (channel === 'wechat') return <ClawBotSettingsPanel onBack={() => setParams({})} />
    if (channel === 'qq-official') return <QQOfficialSettingsPanel onBack={() => setParams({})} />
    if (channel === 'qq') return <NapCatSettingsPanel onBack={() => setParams({})} />
    return <ChannelDirectory />
}

function ChannelDirectory() {
    const { t, locale } = useT()
    const copy = getChannelsCopy(locale)
    const [revision, setRevision] = useState(0)
    const wechat = useManagementResource(clawBotService.getSettings, revision)
    const qq = useManagementResource(napCatService.getSettings, revision)
    const official = useManagementResource(qqOfficialService.getSettings, revision)
    const resources = { wechat, qq, 'qq-official': official }
    const loading = Object.values(resources).some((r) => r.phase === 'loading')
    const failed = Object.values(resources).some((r) => r.phase === 'error')
    const statusLabel = (key: 'wechat' | 'qq' | 'qq-official') => {
        const resource = resources[key]
        if (resource.phase !== 'ready') {
            return t(resource.phase === 'loading' ? 'common.loading' : 'console.unavailable')
        }
        if (key === 'qq-official' && official.phase === 'ready') return t(`qqOfficial.statuses.${official.data.status}`)
        const labels: Record<string, string> = {
            disabled: t('common.disable'),
            missing_token: t('clawBot.missingToken'),
            running: t('clawBot.running'),
            connecting: t('clawBot.connecting'),
            token_expired: t('clawBot.tokenExpired'),
            stopped: t('clawBot.stopped'),
            error: t('clawBot.error'),
            connected: t('napCat.connected'),
            disconnected: t('napCat.disconnected'),
        }
        return labels[resource.data.status] || t('console.unknownStatus')
    }

    return (
        <Page className="channels-page">
            <PageHeader
                title={copy.title}
                description={copy.description}
                actions={
                    <button
                        type="button"
                        className="ui-btn"
                        disabled={loading}
                        onClick={() => setRevision((value) => value + 1)}
                    >
                        <Icon name="refresh" />
                        {copy.refresh}
                    </button>
                }
            />
            {failed && (
                <p className="channel-notice" role="alert">
                    <Icon name="alert" size={16} />
                    {copy.loadFailed}
                </p>
            )}
            <div className="channel-grid">
                {(['wechat', 'qq', 'qq-official'] as const).map((key) => {
                    const resource = resources[key]
                    const settings = resource.phase === 'ready' ? resource.data : null
                    const placeholder = resource.phase === 'loading' ? '—' : t('console.unavailable')
                    return (
                        <article
                            className={`channel-card channel-card--${key}`}
                            key={key}
                            aria-busy={resource.phase === 'loading'}
                            aria-labelledby={`channel-${key}-title`}
                        >
                            <div className="channel-card__head">
                                <span className="channel-card__icon" aria-hidden="true">
                                    <Icon name={key === 'wechat' ? 'message' : 'chat'} size={20} />
                                </span>
                                <div className="channel-card__text">
                                    <h2 id={`channel-${key}-title`}>
                                        {key === 'qq-official'
                                            ? t('qqOfficial.title')
                                            : key === 'wechat'
                                              ? copy.wechat
                                              : copy.qq}
                                    </h2>
                                    <p>
                                        {key === 'qq-official'
                                            ? t('qqOfficial.hint')
                                            : key === 'wechat'
                                              ? copy.wechatHint
                                              : copy.qqHint}
                                    </p>
                                </div>
                                <span role="status" className="channel-card__status">
                                    <Badge tone={settings ? channelStatusTone(settings.status) : 'neutral'} dot>
                                        {statusLabel(key)}
                                    </Badge>
                                </span>
                            </div>
                            <dl className="channel-card__facts">
                                <div>
                                    <dt>{copy.persona}</dt>
                                    <dd>
                                        {settings
                                            ? ('prompt_name' in settings ? settings.prompt_name : settings.prompt_id) ||
                                              copy.defaultPersona
                                            : placeholder}
                                    </dd>
                                </div>
                                <div>
                                    <dt>{copy.connection}</dt>
                                    <dd>
                                        {!settings
                                            ? placeholder
                                            : key === 'wechat' && wechat.phase === 'ready'
                                              ? t(
                                                    wechat.data.polling
                                                        ? 'clawBot.backgroundPolling'
                                                        : 'clawBot.notPolling'
                                                )
                                              : statusLabel(key)}
                                    </dd>
                                </div>
                            </dl>
                            <Link
                                className="channel-card__open"
                                to={`/channels?channel=${key}`}
                                aria-describedby={`channel-${key}-title`}
                            >
                                <span>{copy.configure}</span>
                                <Icon name="chevronRight" size={16} />
                            </Link>
                        </article>
                    )
                })}
            </div>
        </Page>
    )
}
