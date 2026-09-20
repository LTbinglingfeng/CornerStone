import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useT } from '../contexts/I18nContext'
import { useManagementResource } from '../hooks/useManagementResource'
import { clawBotService } from '../services/clawbotService'
import { napCatService } from '../services/napcatService'
import ClawBotSettingsPanel from './ClawBotSettings'
import NapCatSettingsPanel from './NapCatSettings'
import './ChannelsPage.css'

export default function ChannelsPage() {
    const [params, setParams] = useSearchParams()
    const channel = params.get('channel')
    if (channel === 'wechat') return <ClawBotSettingsPanel onBack={() => setParams({})} />
    if (channel === 'qq') return <NapCatSettingsPanel onBack={() => setParams({})} />
    return <ChannelDirectory />
}

function ChannelDirectory() {
    const { t } = useT()
    const [revision, setRevision] = useState(0)
    const wechat = useManagementResource(clawBotService.getSettings, revision)
    const qq = useManagementResource(napCatService.getSettings, revision)
    const resources = { wechat, qq }
    const loading = wechat.phase === 'loading' || qq.phase === 'loading'
    const failed = wechat.phase === 'error' || qq.phase === 'error'
    const statusLabel = (key: 'wechat' | 'qq') => {
        const resource = resources[key]
        if (resource.phase !== 'ready') {
            return t(resource.phase === 'loading' ? 'common.loading' : 'console.unavailable')
        }
        const labels: Record<string, string> = {
            disabled: t('common.disable'),
            missing_token: t('clawBot.missingToken'),
            running: t('clawBot.running'),
            stopped: t('clawBot.stopped'),
            error: t('clawBot.error'),
            connected: t('napCat.connected'),
            disconnected: t('napCat.disconnected'),
        }
        return labels[resource.data.status] || t('console.unknownStatus')
    }

    return (
        <section className="channels-page">
            <header className="console-page-heading">
                <div>
                    <h1>{t('console.channels')}</h1>
                    <p>{t('console.channelsHint')}</p>
                </div>
                <button className="console-button" disabled={loading} onClick={() => setRevision((value) => value + 1)}>
                    {t('console.refresh')}
                </button>
            </header>
            <div className="channel-grid">
                {(['wechat', 'qq'] as const).map((key) => {
                    const resource = resources[key]
                    const settings = resource.phase === 'ready' ? resource.data : null
                    const active = settings?.status === 'running' || settings?.status === 'connected'
                    const placeholder = resource.phase === 'loading' ? '—' : t('console.unavailable')
                    return (
                        <article
                            className={`channel-card ${key === 'wechat' ? 'channel-primary' : ''}`}
                            key={key}
                            aria-busy={resource.phase === 'loading'}
                        >
                            <div className="channel-card-top">
                                <span className="channel-monogram" aria-hidden="true">
                                    {key === 'wechat' ? 'W' : 'Q'}
                                </span>
                                <span className={`channel-state ${active ? 'is-active' : ''}`} role="status">
                                    {statusLabel(key)}
                                </span>
                            </div>
                            <div>
                                <h2>{t(key === 'wechat' ? 'settings.wechatClawBot' : 'settings.qqNapCat')}</h2>
                                <p>{t(key === 'wechat' ? 'console.wechatHint' : 'console.qqHint')}</p>
                            </div>
                            <dl>
                                <div>
                                    <dt>{t('console.boundPersona')}</dt>
                                    <dd>
                                        {settings ? settings.prompt_name || t('console.defaultPersona') : placeholder}
                                    </dd>
                                </div>
                                <div>
                                    <dt>{t('console.connection')}</dt>
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
                            <Link className="console-button" to={`/channels?channel=${key}`}>
                                {t('console.configure')}
                                <span aria-hidden="true">→</span>
                            </Link>
                        </article>
                    )
                })}
            </div>
            {failed && (
                <p className="channel-notice" role="alert">
                    {t('console.channelLoadFailed')}
                </p>
            )}
            <div className="channel-web-note">
                <div>
                    <h2>{t('console.webTesting')}</h2>
                    <p>{t('console.webTestingHint')}</p>
                </div>
                <Link className="console-button" to="/chat">
                    {t('console.chat')}
                </Link>
            </div>
        </section>
    )
}
