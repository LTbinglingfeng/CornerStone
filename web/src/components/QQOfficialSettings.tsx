import { useEffect, useRef, useState } from 'react'
import QRCode from 'qrcode'
import { getPrompts } from '../services/api'
import { qqOfficialService as service } from '../services/qqOfficialService'
import type { QQOfficialSettings, QQOfficialUpdate } from '../types/qqOfficial'
import type { Prompt } from '../types/chat'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { getChannelsCopy } from '../i18n/channels'
import { Badge, Group, Row, Toggle, Icon } from './ui'
import { CustomSelect } from './provider'
import { channelStatusTone } from './channelStatus'
import './ClawBotSettings.css'

export default function QQOfficialSettingsPanel({ onBack }: { onBack: () => void }) {
    const { t, locale } = useT()
    const copy = getChannelsCopy(locale).detail
    const { showToast } = useToast()
    const [settings, setSettings] = useState<QQOfficialSettings | null>(null)
    const [form, setForm] = useState<QQOfficialUpdate>({
        enabled: false,
        app_id: '',
        prompt_id: '',
        source_filter_mode: 'all',
        allowed_user_openids: [],
    })
    const [allowlist, setAllowlist] = useState('')
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [cancelling, setCancelling] = useState(false)
    const needsSync = useRef(false)
    const [qrState, setQRState] = useState<'starting' | 'pending' | 'created' | 'expired' | 'cancelled' | 'error' | ''>(
        ''
    )
    const [qrImage, setQRImage] = useState('')
    const life = useRef<AbortController | null>(null)
    const generation = useRef(0)
    const qrController = useRef<AbortController | null>(null)
    const session = useRef('')
    const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
    const deadlineTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
    const busy = qrState === 'starting' || qrState === 'pending'
    const dirty =
        !!settings &&
        (form.enabled !== settings.enabled ||
            form.app_id !== settings.app_id ||
            form.prompt_id !== settings.prompt_id ||
            form.source_filter_mode !== settings.source_filter_mode ||
            allowlist !== (settings.allowed_user_openids || []).join('\n') ||
            !!form.app_secret ||
            !!form.clear_secret)
    const report = (error: unknown) =>
        showToast(error instanceof Error ? error.message : t('qqOfficial.requestFailed'), 'error')
    const sync = (next: QQOfficialSettings) => {
        needsSync.current = false
        setSettings(next)
        setForm({
            revision: next.revision,
            enabled: next.enabled,
            app_id: next.app_id,
            prompt_id: next.prompt_id,
            source_filter_mode: next.source_filter_mode,
            allowed_user_openids: next.allowed_user_openids || [],
            app_secret: '',
            clear_secret: false,
        })
        setAllowlist((next.allowed_user_openids || []).join('\n'))
    }
    const stop = async () => {
        generation.current++
        qrController.current?.abort()
        clearTimeout(timer.current)
        clearTimeout(deadlineTimer.current)
        const id = session.current
        session.current = ''
        if (!id) return
        setCancelling(true)
        try {
            const result = await service.cancel(id)
            if (result.status === 'created' && !life.current?.signal.aborted) {
                needsSync.current = true
                sync(await service.getSettings(life.current?.signal))
                setQRState('created')
            }
            return result
        } finally {
            if (!life.current?.signal.aborted) setCancelling(false)
        }
    }
    const load = async (signal: AbortSignal) => {
        setLoading(true)
        try {
            const [next, list] = await Promise.all([service.getSettings(signal), getPrompts()])
            if (!signal.aborted) {
                sync(next)
                setPrompts(list)
            }
        } catch (error) {
            if (!signal.aborted) report(error)
        } finally {
            if (!signal.aborted) setLoading(false)
        }
    }
    useEffect(() => {
        const controller = new AbortController()
        life.current = controller
        void load(controller.signal)
        return () => {
            controller.abort()
            void stop().catch(() => {})
        }
    }, [])

    const start = async () => {
        if (busy || saving || cancelling || needsSync.current) return
        if (dirty) {
            showToast(t('qqOfficial.saveBeforeScan'), 'error')
            return
        }
        setQRState('starting')
        setQRImage('')
        const cancellation = stop()
        const current = generation.current
        try {
            await cancellation
        } catch (error) {
            if (generation.current !== current || life.current?.signal.aborted) return
            report(error)
            setQRState('error')
            return
        }
        if (life.current?.signal.aborted || generation.current !== current) return
        const controller = new AbortController()
        qrController.current = controller
        const active = () =>
            generation.current === current && !controller.signal.aborted && !life.current?.signal.aborted
        // Even a stalled network request cannot keep this flow alive indefinitely.
        deadlineTimer.current = setTimeout(
            () => {
                if (!active()) return
                void stop().catch(report)
                setQRState('expired')
                setQRImage('')
            },
            5 * 60 * 1000
        )
        try {
            const login = await service.start(controller.signal)
            if (!active()) {
                void service.cancel(login.id).catch(() => {})
                return
            }
            session.current = login.id
            const expires = Date.parse(login.expires_at)
            const remaining = Math.min(Number.isFinite(expires) ? expires - Date.now() : 0, 5 * 60 * 1000)
            if (remaining <= 0) {
                void stop().catch(report)
                setQRState('expired')
                return
            }
            clearTimeout(deadlineTimer.current)
            deadlineTimer.current = setTimeout(() => {
                if (!active()) return
                void stop().catch(report)
                setQRState('expired')
                setQRImage('')
            }, remaining)
            const image = await QRCode.toDataURL(login.qr_url, { width: 240, margin: 2 })
            if (!active()) return
            setQRImage(image)
            setQRState('pending')
            const interval = Math.max(1, Math.min(30, login.interval_seconds || 3)) * 1000
            const poll = async () => {
                try {
                    const result = await service.poll(login.id, controller.signal)
                    if (!active()) return
                    if (result.status === 'pending') {
                        timer.current = setTimeout(() => void poll(), interval)
                        return
                    }
                    clearTimeout(deadlineTimer.current)
                    session.current = ''
                    setQRImage('')
                    if (result.status === 'created') {
                        needsSync.current = true
                        deadlineTimer.current = setTimeout(() => {
                            if (!active()) return
                            void stop().catch(report)
                            setQRState('error')
                            showToast(t('qqOfficial.requestFailed'), 'error')
                        }, 20000)
                        const next = await service.getSettings(controller.signal)
                        if (!active()) return
                        clearTimeout(deadlineTimer.current)
                        sync(next)
                        setQRState('created')
                        showToast(t('qqOfficial.created'), 'success')
                    } else {
                        setQRState(result.status)
                        if (result.error) report(new Error(result.error))
                    }
                } catch (error) {
                    if (!active()) return
                    void stop().catch(report)
                    setQRState('error')
                    setQRImage('')
                    report(error)
                }
            }
            timer.current = setTimeout(() => void poll(), interval)
        } catch (error) {
            if (!active()) return
            void stop().catch(report)
            setQRState('error')
            setQRImage('')
            report(error)
        }
    }
    const save = async () => {
        if (saving || busy || cancelling || needsSync.current) return
        setSaving(true)
        try {
            const cancelled = await stop()
            if (cancelled?.status === 'created') return
            if (life.current?.signal.aborted) return
            setQRState('')
            setQRImage('')
            const next = await service.updateSettings(
                {
                    ...form,
                    app_id: form.app_id.trim(),
                    app_secret: form.app_secret?.trim() || undefined,
                    allowed_user_openids: [
                        ...new Set(
                            allowlist
                                .split('\n')
                                .map((s) => s.trim())
                                .filter(Boolean)
                        ),
                    ],
                },
                life.current?.signal
            )
            if (!life.current?.signal.aborted) {
                sync(next)
                showToast(t('qqOfficial.saved'), 'success')
            }
        } catch (error) {
            if (!life.current?.signal.aborted) report(error)
        } finally {
            if (!life.current?.signal.aborted) setSaving(false)
        }
    }
    const refreshStatus = async () => {
        const current = generation.current
        try {
            const next = await service.getSettings(life.current?.signal)
            if (life.current?.signal.aborted || current !== generation.current) return
            if (needsSync.current) {
                sync(next)
                return
            }
            setSettings((previous) =>
                previous
                    ? {
                          ...previous,
                          status: next.status,
                          last_error: next.last_error,
                          bot_id: next.bot_id,
                          bot_name: next.bot_name,
                      }
                    : previous
            )
        } catch (error) {
            if (!life.current?.signal.aborted) report(error)
        }
    }
    const disabled = saving || busy || cancelling
    return (
        <div className="provider-settings channel-detail">
            <div className="provider-settings-header">
                <button className="back-button" onClick={onBack} aria-label={t('common.back')}>
                    <Icon name="chevronLeft" />
                </button>
                <div className="provider-settings-title">{t('qqOfficial.title')}</div>
                {settings && (
                    <div className="channel-detail-actions">
                        <button
                            className="save-button ui-btn ui-btn--primary ui-btn--sm"
                            onClick={() => void save()}
                            disabled={disabled}
                        >
                            {saving ? t('common.saving') : t('napCat.saveSettings')}
                        </button>
                    </div>
                )}
            </div>
            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : !settings ? (
                <div className="console-load-error" role="alert">
                    <p>{copy.loadFailed}</p>
                    <button className="ui-btn" onClick={() => life.current && void load(life.current.signal)}>
                        {t('console.refresh')}
                    </button>
                </div>
            ) : (
                <div className="provider-settings-content">
                    <Group
                        title={copy.status}
                        footer={t('qqOfficial.scope')}
                        action={
                            <button
                                className="ui-btn ui-btn--sm"
                                disabled={disabled}
                                onClick={() => void refreshStatus()}
                            >
                                {t('console.refresh')}
                            </button>
                        }
                    >
                        <Row
                            label={t('napCat.runStatus')}
                            trailing={
                                <Badge tone={channelStatusTone(settings.status)} dot>
                                    {t(`qqOfficial.statuses.${settings.status}`)}
                                </Badge>
                            }
                        />
                        <Row label={t('napCat.nickname')} value={settings.bot_name || '—'} />
                        <Row label="Bot ID" value={settings.bot_id || '—'} />
                        {settings.last_error && (
                            <div className="channel-field channel-error" role="alert">
                                {settings.last_error}
                            </div>
                        )}
                    </Group>
                    <Group
                        title={t('qqOfficial.scan')}
                        footer={t(dirty ? 'qqOfficial.saveBeforeScan' : 'qqOfficial.scanHint')}
                    >
                        <div className="channel-field">
                            <button className="ui-btn" onClick={() => void start()} disabled={disabled || dirty}>
                                {t(qrState === 'expired' ? 'qqOfficial.refresh' : 'qqOfficial.start')}
                            </button>
                            {busy && (
                                <button
                                    className="ui-btn ui-btn--ghost"
                                    onClick={() => {
                                        void stop().catch(report)
                                        setQRState('cancelled')
                                        setQRImage('')
                                    }}
                                >
                                    {t('qqOfficial.cancel')}
                                </button>
                            )}
                        </div>
                        {qrState && (
                            <div className="channel-field clawbot-qrcode">
                                <p role="status">{t(`qqOfficial.${qrState}`)}</p>
                                {qrImage && (
                                    <img className="clawbot-qrcode-image" src={qrImage} alt={t('qqOfficial.qrAlt')} />
                                )}
                            </div>
                        )}
                    </Group>
                    <Group title={t('qqOfficial.manual')} footer={t('qqOfficial.idsHint')}>
                        <Row
                            label={t('qqOfficial.enable')}
                            trailing={
                                <Toggle
                                    label={t('qqOfficial.enable')}
                                    checked={form.enabled}
                                    disabled={disabled}
                                    onChange={(enabled) => setForm((f) => ({ ...f, enabled }))}
                                />
                            }
                        />
                        <div className="channel-field">
                            <div className="ui-field">
                                <label className="ui-field__label" htmlFor="qq-official-app">
                                    AppID
                                </label>
                                <input
                                    id="qq-official-app"
                                    className="ui-input"
                                    value={form.app_id}
                                    disabled={disabled}
                                    onChange={(e) => setForm((f) => ({ ...f, app_id: e.target.value }))}
                                />
                            </div>
                        </div>
                        <div className="channel-field">
                            <div className="ui-field">
                                <label className="ui-field__label" htmlFor="qq-official-secret">
                                    {t('qqOfficial.secret')}
                                </label>
                                <input
                                    id="qq-official-secret"
                                    className="ui-input"
                                    type="password"
                                    autoComplete="new-password"
                                    value={form.app_secret || ''}
                                    disabled={disabled || form.clear_secret}
                                    onChange={(e) => setForm((f) => ({ ...f, app_secret: e.target.value }))}
                                />
                                <span className="ui-field__hint">
                                    {t(settings.has_secret ? 'qqOfficial.secretSaved' : 'qqOfficial.secretEmpty')}
                                </span>
                            </div>
                        </div>
                        {settings.has_secret && (
                            <Row
                                label={t('qqOfficial.clear')}
                                trailing={
                                    <Toggle
                                        label={t('qqOfficial.clear')}
                                        checked={!!form.clear_secret}
                                        disabled={disabled}
                                        onChange={(clear_secret) =>
                                            setForm((f) => ({ ...f, clear_secret, app_secret: '' }))
                                        }
                                    />
                                }
                            />
                        )}
                    </Group>
                    <Group title={copy.basics} className="channel-group--overflow">
                        <div className="channel-field">
                            <div className="ui-field">
                                <span className="ui-field__label">{t('napCat.bindPersona')}</span>
                                <CustomSelect
                                    ariaLabel={t('napCat.bindPersona')}
                                    disabled={disabled}
                                    value={form.prompt_id}
                                    options={[
                                        { value: '', label: t('napCat.noPersonaBind') },
                                        ...prompts.map((p) => ({ value: p.id, label: p.name })),
                                    ]}
                                    onChange={(prompt_id) => setForm((f) => ({ ...f, prompt_id }))}
                                />
                            </div>
                        </div>
                    </Group>
                    <Group title={copy.access} className="channel-group--overflow">
                        <div className="channel-field">
                            <div className="ui-field">
                                <span className="ui-field__label">{t('napCat.sourceFilterMode')}</span>
                                <CustomSelect
                                    ariaLabel={t('napCat.sourceFilterMode')}
                                    disabled={disabled}
                                    value={form.source_filter_mode}
                                    options={[
                                        { value: 'all', label: t('napCat.filterAll') },
                                        { value: 'allowlist', label: t('napCat.filterAllowlist') },
                                    ]}
                                    onChange={(value) =>
                                        setForm((f) => ({
                                            ...f,
                                            source_filter_mode: value === 'allowlist' ? 'allowlist' : 'all',
                                        }))
                                    }
                                />
                            </div>
                        </div>
                        <div className="channel-field">
                            <div className="ui-field">
                                <label className="ui-field__label" htmlFor="qq-official-users">
                                    {t('qqOfficial.allowlist')}
                                </label>
                                <textarea
                                    id="qq-official-users"
                                    className="ui-textarea channel-allowlist"
                                    value={allowlist}
                                    disabled={disabled || form.source_filter_mode !== 'allowlist'}
                                    onChange={(e) => setAllowlist(e.target.value)}
                                />
                            </div>
                        </div>
                    </Group>
                </div>
            )}
        </div>
    )
}
