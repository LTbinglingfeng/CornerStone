import { useEffect, useMemo, useRef, useState } from 'react'
import { motion } from 'motion/react'
import QRCode from 'qrcode'
import { getPrompts } from '../services/api'
import {
    clawBotService,
    type ClawBotCommandPermissionKey,
    type ClawBotCommandPermissions,
    type ClawBotQRCodeStartResponse,
    type ClawBotSettings,
} from '../services/clawbotService'
import { useT } from '../contexts/I18nContext'
import type { Prompt } from '../types/chat'
import { CustomSelect } from './provider'
import { useToast } from '../contexts/ToastContext'
import { drawerVariants } from '../utils/motion'
import { getChannelsCopy } from '../i18n/channels'
import { Badge, Group, Row, Toggle } from './ui'
import { channelStatusTone } from './channelStatus'
import './ClawBotSettings.css'

interface ClawBotSettingsProps {
    onBack: () => void
}

type ClawBotFormState = {
    enabled: boolean
    base_url: string
    bot_token: string
    prompt_id: string
    clear_bot_token: boolean
    command_permissions: ClawBotCommandPermissions
}

const DEFAULT_COMMAND_PERMISSIONS: ClawBotCommandPermissions = {
    new: true,
    ls: true,
    checkout: true,
    rename: true,
    delete: true,
    prompt: true,
    re: true,
}

const normalizeCommandPermissions = (
    permissions?: Partial<Record<ClawBotCommandPermissionKey, boolean>>
): ClawBotCommandPermissions => ({
    ...DEFAULT_COMMAND_PERMISSIONS,
    ...(permissions || {}),
})

const ClawBotSettingsPanel: React.FC<ClawBotSettingsProps> = ({ onBack }) => {
    const { t, locale } = useT()
    const copy = getChannelsCopy(locale).detail
    const { showToast } = useToast()
    const pollTimerRef = useRef<number | null>(null)
    const qrGenerationRef = useRef(0)
    const qrControllerRef = useRef<AbortController | null>(null)
    const lifecycleRef = useRef<AbortController | null>(null)
    const settingsRevisionRef = useRef(0)
    const [verifyCode, setVerifyCode] = useState('')
    const [qrPolling, setQRPolling] = useState(false)
    const [qrRetry, setQRRetry] = useState(false)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [settings, setSettings] = useState<ClawBotSettings | null>(null)
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [form, setForm] = useState<ClawBotFormState>({
        enabled: false,
        base_url: 'https://ilinkai.weixin.qq.com',
        bot_token: '',
        prompt_id: '',
        clear_bot_token: false,
        command_permissions: DEFAULT_COMMAND_PERMISSIONS,
    })
    const [qrData, setQRData] = useState<ClawBotQRCodeStartResponse | null>(null)
    const [qrStatus, setQRStatus] = useState('')
    const [qrLoading, setQRLoading] = useState(false)
    const [qrDisplaySrc, setQRDisplaySrc] = useState('')

    const cancelQRCode = () => {
        qrGenerationRef.current += 1
        qrControllerRef.current?.abort()
        if (pollTimerRef.current != null) window.clearTimeout(pollTimerRef.current)
        pollTimerRef.current = null
    }

    useEffect(() => {
        const controller = new AbortController()
        lifecycleRef.current = controller
        void loadData()
        let refreshTimer: number | undefined
        const refresh = async () => {
            const revision = settingsRevisionRef.current
            try {
                const nextSettings = await clawBotService.getSettings(controller.signal)
                if (!controller.signal.aborted && revision === settingsRevisionRef.current) {
                    setSettings(nextSettings)
                }
            } catch {
                // Background refresh must not interrupt editing or QR login.
            } finally {
                if (!controller.signal.aborted) refreshTimer = window.setTimeout(refresh, 10000)
            }
        }
        refreshTimer = window.setTimeout(refresh, 10000)
        return () => {
            controller.abort()
            window.clearTimeout(refreshTimer)
            cancelQRCode()
        }
    }, [])

    useEffect(() => {
        let disposed = false

        setQRDisplaySrc('')

        if (!qrData) {
            return
        }

        void (async () => {
            const text = (qrData.qrcode_img_content || qrData.qrcode || '').trim()
            if (!text) return

            try {
                const qrDataUrl = await QRCode.toDataURL(text, {
                    width: 320,
                    margin: 1,
                })
                if (disposed) return
                setQRDisplaySrc(qrDataUrl)
            } catch {
                if (!disposed) {
                    setQRDisplaySrc('')
                }
            }
        })()

        return () => {
            disposed = true
        }
    }, [qrData])

    const promptOptions = useMemo(
        () => [
            { value: '', label: t('clawBot.noPersonaBind') },
            ...prompts.map((prompt) => ({ value: prompt.id, label: prompt.name })),
        ],
        [prompts, t]
    )

    const commandPermissionItems = useMemo(
        () =>
            [
                {
                    key: 'new',
                    label: t('clawBot.commandNewLabel'),
                    description: t('clawBot.commandNewDesc'),
                },
                {
                    key: 'ls',
                    label: t('clawBot.commandListLabel'),
                    description: t('clawBot.commandListDesc'),
                },
                {
                    key: 'checkout',
                    label: t('clawBot.commandCheckoutLabel'),
                    description: t('clawBot.commandCheckoutDesc'),
                },
                {
                    key: 'rename',
                    label: t('clawBot.commandRenameLabel'),
                    description: t('clawBot.commandRenameDesc'),
                },
                {
                    key: 'delete',
                    label: t('clawBot.commandDeleteLabel'),
                    description: t('clawBot.commandDeleteDesc'),
                },
                {
                    key: 'prompt',
                    label: t('clawBot.commandPromptLabel'),
                    description: t('clawBot.commandPromptDesc'),
                },
                {
                    key: 're',
                    label: t('clawBot.commandRegenerateLabel'),
                    description: t('clawBot.commandRegenerateDesc'),
                },
            ] satisfies Array<{
                key: ClawBotCommandPermissionKey
                label: string
                description: string
            }>,
        [t]
    )

    const loadData = async () => {
        const signal = lifecycleRef.current!.signal
        setLoading(true)
        try {
            const [nextSettings, promptList] = await Promise.all([clawBotService.getSettings(signal), getPrompts()])
            if (signal.aborted) return
            setSettings(nextSettings)
            setPrompts(promptList)
            setForm({
                enabled: nextSettings.enabled,
                base_url: nextSettings.base_url || 'https://ilinkai.weixin.qq.com',
                bot_token: '',
                prompt_id: nextSettings.prompt_id || '',
                clear_bot_token: false,
                command_permissions: normalizeCommandPermissions(nextSettings.command_permissions),
            })
        } catch (error) {
            if (signal.aborted) return
            const message = error instanceof Error ? error.message : t('common.loadFailed')
            showToast(message, 'error')
        } finally {
            if (!signal.aborted) setLoading(false)
        }
    }

    const syncSettings = (nextSettings: ClawBotSettings) => {
        settingsRevisionRef.current += 1
        setSettings(nextSettings)
        setForm((current) => ({
            ...current,
            enabled: nextSettings.enabled,
            base_url: nextSettings.base_url || current.base_url || 'https://ilinkai.weixin.qq.com',
            bot_token: '',
            prompt_id: nextSettings.prompt_id || '',
            clear_bot_token: false,
            command_permissions: normalizeCommandPermissions(nextSettings.command_permissions),
        }))
    }

    const pollQRCode = async (
        sessionID: string,
        generation: number,
        controller: AbortController,
        code?: string,
        failures = 0
    ) => {
        const active = () => generation === qrGenerationRef.current && !controller.signal.aborted
        if (!active()) return
        setQRPolling(true)
        setQRRetry(false)
        try {
            const result = await clawBotService.pollQRCode(sessionID, code, controller.signal)
            if (!active()) return
            setQRStatus(result.status)
            if (result.status === 'confirmed' || result.status === 'binded_redirect') {
                if (result.settings) syncSettings(result.settings)
                setQRData(null)
                setVerifyCode('')
                showToast(
                    t(result.status === 'binded_redirect' ? 'clawBot.qrAlreadyBound' : 'clawBot.qrConfirmed'),
                    'success'
                )
                return
            }
            if (['need_verifycode', 'verify_code_blocked', 'expired'].includes(result.status)) return
            if (!['wait', 'scaned', 'scaned_but_redirect'].includes(result.status)) {
                setQRRetry(true)
                return
            }
            setVerifyCode('')
            pollTimerRef.current = window.setTimeout(() => void pollQRCode(sessionID, generation, controller), 2500)
        } catch {
            if (!active()) return
            // Never automatically replay a verification code: the server may have consumed it.
            if (code === undefined && failures < 3) {
                pollTimerRef.current = window.setTimeout(
                    () => void pollQRCode(sessionID, generation, controller, undefined, failures + 1),
                    2500
                )
            } else {
                setQRRetry(true)
            }
        } finally {
            if (active()) setQRPolling(false)
        }
    }

    const retryQRCode = (code?: string) => {
        if (!qrData || qrPolling || !qrControllerRef.current) return
        if (pollTimerRef.current != null) window.clearTimeout(pollTimerRef.current)
        void pollQRCode(qrData.session_id, qrGenerationRef.current, qrControllerRef.current, code)
    }

    const handleSave = async () => {
        if (saving) return
        cancelQRCode()
        setQRData(null)
        setQRStatus('')
        setVerifyCode('')
        setQRRetry(false)
        setQRPolling(false)
        setQRLoading(false)
        const signal = lifecycleRef.current!.signal
        setSaving(true)
        try {
            const nextSettings = await clawBotService.updateSettings(
                {
                    enabled: form.enabled,
                    base_url: form.base_url.trim(),
                    bot_token: form.bot_token.trim() || undefined,
                    prompt_id: form.prompt_id || undefined,
                    clear_bot_token: form.clear_bot_token,
                    command_permissions: form.command_permissions,
                },
                signal
            )
            if (signal.aborted) return
            syncSettings(nextSettings)
            showToast(t('clawBot.saved'), 'success')
        } catch (error) {
            if (signal.aborted) return
            const message = error instanceof Error ? error.message : t('common.saveFailed')
            showToast(message, 'error')
        } finally {
            if (!signal.aborted) setSaving(false)
        }
    }

    const handleStartQRCode = async () => {
        if (qrLoading) return
        cancelQRCode()
        const generation = qrGenerationRef.current
        const controller = new AbortController()
        qrControllerRef.current = controller
        const active = () => generation === qrGenerationRef.current && !controller.signal.aborted
        setQRData(null)
        setQRStatus('')
        setVerifyCode('')
        setQRRetry(false)
        setQRPolling(false)
        setQRLoading(true)
        try {
            const nextSettings = await clawBotService.updateSettings(
                {
                    enabled: form.enabled,
                    base_url: form.base_url.trim(),
                    prompt_id: form.prompt_id || undefined,
                    clear_bot_token: false,
                    command_permissions: form.command_permissions,
                },
                controller.signal
            )
            if (!active()) return
            syncSettings(nextSettings)

            const result = await clawBotService.startQRCode(form.base_url.trim(), controller.signal)
            if (!active()) return
            setQRData(result)
            setQRStatus('wait')
            pollTimerRef.current = window.setTimeout(
                () => void pollQRCode(result.session_id, generation, controller),
                2500
            )
        } catch (error) {
            if (!active()) return
            const message = error instanceof Error ? error.message : t('service.getQRCodeFailed')
            showToast(message, 'error')
        } finally {
            if (active()) setQRLoading(false)
        }
    }

    const qrStatusLabels: Record<string, string> = {
        wait: t('clawBot.qrWait'),
        scaned: t('clawBot.qrScanned'),
        confirmed: t('clawBot.qrConfirmed'),
        expired: t('clawBot.qrExpired'),
        need_verifycode: t('clawBot.qrNeedCode'),
        verify_code_blocked: t('clawBot.qrCodeBlocked'),
        scaned_but_redirect: t('clawBot.qrRedirect'),
        binded_redirect: t('clawBot.qrAlreadyBound'),
    }

    const currentStatus = settings?.status || 'disabled'
    const statusLabelMap: Record<string, string> = {
        disabled: t('common.disable'),
        missing_token: t('clawBot.missingToken'),
        running: t('clawBot.running'),
        connecting: t('clawBot.connecting'),
        token_expired: t('clawBot.tokenExpired'),
        error: t('clawBot.error'),
        stopped: t('clawBot.stopped'),
    }
    const statusLabel = statusLabelMap[currentStatus] || currentStatus || t('common.notConfigured')

    return (
        <motion.div
            className="provider-settings clawbot-settings channel-detail"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={drawerVariants}
        >
            <div className="provider-settings-header">
                <button className="back-button" onClick={onBack} aria-label={t('common.back')}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('clawBot.title')}</div>
                {settings && !loading && (
                    <div className="clawbot-actions channel-detail-actions">
                        <button
                            type="button"
                            className="save-button ui-btn ui-btn--primary ui-btn--sm"
                            onClick={handleSave}
                            disabled={saving}
                        >
                            {saving ? t('common.saving') : t('clawBot.saveSettings')}
                        </button>
                    </div>
                )}
            </div>

            {loading ? (
                <div className="provider-settings-loading">
                    <span className="ui-spinner" aria-hidden="true" />
                    {t('common.loading')}
                </div>
            ) : !settings ? (
                <div className="console-load-error" role="alert">
                    <p>{copy.loadFailed}</p>
                    <button className="ui-btn" onClick={() => void loadData()}>
                        {t('console.refresh')}
                    </button>
                </div>
            ) : (
                <div className="provider-settings-content">
                    <Group title={copy.status}>
                        <Row
                            label={copy.state}
                            trailing={
                                <Badge tone={channelStatusTone(currentStatus)} dot>
                                    {statusLabel}
                                </Badge>
                            }
                        />
                        <Row
                            label={copy.polling}
                            value={settings.polling ? t('clawBot.backgroundPolling') : t('clawBot.notPolling')}
                        />
                        {settings.ilink_user_id && <Row label={copy.account} value={settings.ilink_user_id} />}
                        {settings.last_error && (
                            <div className="channel-field channel-error" role="alert">
                                <span className="channel-error__label">{copy.lastError}</span>
                                <span className="channel-error__text">{settings.last_error}</span>
                            </div>
                        )}
                    </Group>

                    <Group title={copy.connection}>
                        <Row
                            icon="qr"
                            label={copy.scan}
                            detail={copy.scanHint}
                            trailing={
                                <button
                                    type="button"
                                    className="ui-btn ui-btn--sm clawbot-scan-button"
                                    onClick={handleStartQRCode}
                                    disabled={qrLoading || saving}
                                >
                                    {qrLoading ? t('clawBot.getting') : t('clawBot.scanToGet')}
                                </button>
                            }
                        />
                        {qrData && (
                            <div className="channel-field clawbot-qrcode">
                                <div className="clawbot-qrcode-header">
                                    <span className="clawbot-qrcode-title">{t('clawBot.scanLogin')}</span>
                                    <span className="clawbot-qrcode-status" role="status">
                                        {qrStatusLabels[qrStatus] || qrStatus}
                                    </span>
                                </div>
                                <div className="clawbot-qrcode-body">
                                    {qrDisplaySrc ? (
                                        <img
                                            className="clawbot-qrcode-image"
                                            src={qrDisplaySrc}
                                            alt="ClawBot QR Code"
                                        />
                                    ) : qrData.qrcode_img_content || qrData.qrcode ? (
                                        <div className="clawbot-qrcode-loading">
                                            <span className="ui-spinner" aria-hidden="true" />
                                            {t('clawBot.qrGenerating')}
                                        </div>
                                    ) : (
                                        <textarea
                                            className="ui-textarea clawbot-qrcode-text"
                                            readOnly
                                            aria-label={t('clawBot.scanLogin')}
                                            value={qrData.qrcode_img_content || qrData.qrcode}
                                        />
                                    )}
                                </div>
                                {qrStatus === 'need_verifycode' && (
                                    <form
                                        className="clawbot-verify-form"
                                        onSubmit={(event) => {
                                            event.preventDefault()
                                            if (verifyCode.trim()) retryQRCode(verifyCode.trim())
                                        }}
                                    >
                                        <label className="ui-field__label" htmlFor="clawbot-verify-code">
                                            {t('clawBot.qrVerifyCode')}
                                        </label>
                                        <div className="clawbot-verify-row">
                                            <input
                                                id="clawbot-verify-code"
                                                className="ui-input"
                                                value={verifyCode}
                                                onChange={(event) => setVerifyCode(event.target.value)}
                                                autoComplete="one-time-code"
                                                inputMode="numeric"
                                                pattern="[0-9]*"
                                                maxLength={32}
                                                disabled={qrPolling}
                                            />
                                            <button
                                                className="ui-btn"
                                                type="submit"
                                                disabled={qrPolling || !verifyCode.trim()}
                                            >
                                                {t('clawBot.qrSubmitCode')}
                                            </button>
                                        </div>
                                    </form>
                                )}
                                {qrRetry && (
                                    <div className="clawbot-qrcode-retry" role="alert">
                                        <p>{t('clawBot.qrRetryHint')}</p>
                                        <button
                                            type="button"
                                            className="ui-btn ui-btn--sm"
                                            onClick={() => retryQRCode()}
                                            disabled={qrPolling}
                                        >
                                            {t('clawBot.qrRetry')}
                                        </button>
                                    </div>
                                )}
                                <p className="clawbot-qrcode-hint">{t('clawBot.qrCodeHint')}</p>
                            </div>
                        )}
                        <div className="channel-field">
                            <div className="ui-field">
                                <label className="ui-field__label" htmlFor="clawbot-bot-token">
                                    {copy.manualToken}
                                </label>
                                <input
                                    id="clawbot-bot-token"
                                    className="ui-input"
                                    type="password"
                                    autoComplete="off"
                                    aria-label={t('clawBot.botToken')}
                                    value={form.bot_token}
                                    onChange={(event) =>
                                        setForm((current) => ({ ...current, bot_token: event.target.value }))
                                    }
                                    placeholder={
                                        settings.has_bot_token
                                            ? t('clawBot.tokenKeepHint')
                                            : t('clawBot.tokenPlaceholder')
                                    }
                                    disabled={saving}
                                />
                                <div className="channel-token-row">
                                    <span className="ui-field__hint">
                                        {settings.has_bot_token
                                            ? t('clawBot.savedToken', { token: settings.bot_token || '****' })
                                            : t('clawBot.currentNotSavedToken')}
                                    </span>
                                    {settings.has_bot_token && (
                                        <button
                                            type="button"
                                            className={`ui-btn ui-btn--sm ${form.clear_bot_token ? 'ui-btn--danger-solid' : 'ui-btn--ghost'}`}
                                            aria-pressed={form.clear_bot_token}
                                            onClick={() =>
                                                setForm((current) => ({
                                                    ...current,
                                                    clear_bot_token: !current.clear_bot_token,
                                                    bot_token: '',
                                                }))
                                            }
                                        >
                                            {form.clear_bot_token
                                                ? t('clawBot.markedCleared')
                                                : t('clawBot.clearToken')}
                                        </button>
                                    )}
                                </div>
                            </div>
                        </div>
                    </Group>

                    <Group title={copy.basics} className="channel-group--overflow">
                        <Row
                            label={t('clawBot.enableChannel')}
                            detail={copy.enableHintWechat}
                            trailing={
                                <Toggle
                                    label={t('clawBot.enableChannel')}
                                    checked={form.enabled}
                                    onChange={(enabled) => setForm((current) => ({ ...current, enabled }))}
                                    disabled={saving}
                                />
                            }
                        />
                        <div className="channel-field">
                            <div className="ui-field">
                                <label className="ui-field__label" htmlFor="clawbot-base-url">
                                    {t('clawBot.baseUrl')}
                                </label>
                                <input
                                    id="clawbot-base-url"
                                    className="ui-input"
                                    aria-label={t('clawBot.baseUrl')}
                                    value={form.base_url}
                                    onChange={(event) =>
                                        setForm((current) => ({ ...current, base_url: event.target.value }))
                                    }
                                    placeholder="https://ilinkai.weixin.qq.com"
                                    disabled={saving}
                                />
                            </div>
                        </div>
                        <div className="channel-field">
                            <div className="ui-field">
                                <span className="ui-field__label">{t('clawBot.bindPersona')}</span>
                                <CustomSelect
                                    value={form.prompt_id}
                                    options={promptOptions}
                                    onChange={(value) => setForm((current) => ({ ...current, prompt_id: value }))}
                                    ariaLabel={t('clawBot.bindPersona')}
                                    disabled={saving}
                                />
                            </div>
                        </div>
                    </Group>

                    <Group title={copy.commands} description={t('clawBot.commandPermissionsHint')}>
                        {commandPermissionItems.map((item) => (
                            <Row
                                key={item.key}
                                className="channel-command-row"
                                label={<code className="channel-command">{item.label}</code>}
                                detail={item.description}
                                trailing={
                                    <Toggle
                                        label={item.label}
                                        checked={form.command_permissions[item.key]}
                                        onChange={(checked) =>
                                            setForm((current) => ({
                                                ...current,
                                                command_permissions: {
                                                    ...current.command_permissions,
                                                    [item.key]: checked,
                                                },
                                            }))
                                        }
                                        disabled={saving}
                                    />
                                }
                            />
                        ))}
                    </Group>
                </div>
            )}
        </motion.div>
    )
}

export default ClawBotSettingsPanel
