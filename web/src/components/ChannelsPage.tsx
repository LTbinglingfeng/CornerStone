import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useT } from '../contexts/I18nContext'
import { clawBotService, type ClawBotSettings } from '../services/clawbotService'
import { napCatService, type NapCatSettings } from '../services/napcatService'
import ClawBotSettingsPanel from './ClawBotSettings'
import NapCatSettingsPanel from './NapCatSettings'
import './ChannelsPage.css'

type ChannelState = { wechat: ClawBotSettings | null; qq: NapCatSettings | null }

export default function ChannelsPage() {
    const { t } = useT()
    const [params, setParams] = useSearchParams()
    const channel = params.get('channel')
    const [data, setData] = useState<ChannelState>({ wechat: null, qq: null })
    const [loading, setLoading] = useState(true)
    const [failed, setFailed] = useState<string[]>([])
    const [refresh, setRefresh] = useState(0)

    useEffect(() => {
        if (channel === 'wechat' || channel === 'qq') return
        let cancelled = false
        setLoading(true)
        setFailed([])
        void Promise.allSettled([clawBotService.getSettings(), napCatService.getSettings()]).then(([wechat, qq]) => {
            if (cancelled) return
            setData({
                wechat: wechat.status === 'fulfilled' ? wechat.value : null,
                qq: qq.status === 'fulfilled' ? qq.value : null,
            })
            setFailed([
                ...(wechat.status === 'rejected' ? ['wechat'] : []),
                ...(qq.status === 'rejected' ? ['qq'] : []),
            ])
            setLoading(false)
        })
        return () => {
            cancelled = true
        }
    }, [channel, refresh])

    if (channel === 'wechat') return <ClawBotSettingsPanel onBack={() => setParams({})} />
    if (channel === 'qq') return <NapCatSettingsPanel onBack={() => setParams({})} />

    const statusLabel = (key: 'wechat' | 'qq') => {
        if (loading) return t('common.loading')
        if (failed.includes(key) || !data[key]) return t('console.unavailable')
        const status = data[key].status
        const labels: Record<string, string> = {
            disabled: t('common.disable'),
            missing_token: t('clawBot.missingToken'),
            running: t('clawBot.running'),
            stopped: t('clawBot.stopped'),
            error: t('clawBot.error'),
            connected: t('napCat.connected'),
            disconnected: t('napCat.disconnected'),
        }
        return labels[status] || t('console.unknownStatus')
    }

    return (
        <section className="channels-page">
            <header className="console-page-heading">
                <div>
                    <h1>{t('console.channels')}</h1>
                    <p>{t('console.channelsHint')}</p>
                </div>
                <button className="console-button" disabled={loading} onClick={() => setRefresh((value) => value + 1)}>
                    {t('console.refresh')}
                </button>
            </header>
            <div className="channel-grid" aria-busy={loading}>
                {(['wechat', 'qq'] as const).map((key) => {
                    const settings = data[key]
                    const active =
                        !loading &&
                        !failed.includes(key) &&
                        (settings?.status === 'running' || settings?.status === 'connected')
                    return (
                        <article className={`channel-card ${key === 'wechat' ? 'channel-primary' : ''}`} key={key}>
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
                                        {loading
                                            ? '—'
                                            : failed.includes(key)
                                              ? t('console.unavailable')
                                              : settings?.prompt_name || t('console.defaultPersona')}
                                    </dd>
                                </div>
                                <div>
                                    <dt>{t('console.connection')}</dt>
                                    <dd>
                                        {loading
                                            ? '—'
                                            : failed.includes(key)
                                              ? t('console.unavailable')
                                              : key === 'wechat'
                                                ? t(
                                                      data.wechat?.polling
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
            {failed.length > 0 && (
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
