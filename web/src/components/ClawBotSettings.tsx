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
import './ProviderSettings.css'
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
    const { t } = useT()
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
        disabled: t('clawBot.disabled'),
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
            className="provider-settings clawbot-settings"
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
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : !settings ? (
                <div className="console-load-error" role="alert">
                    <p>{t('common.loadFailed')}</p>
                    <button className="console-button" onClick={() => void loadData()}>
                        {t('console.refresh')}
                    </button>
                </div>
            ) : (
                <div className="provider-settings-content">
                    {/* ── 运行状态卡片 ── */}
                    <div className={`clawbot-status-card status-${currentStatus}`}>
                        <div className="clawbot-status-top">
                            <div className="clawbot-status-left">
                                <div className="clawbot-status-icon">
                                    <svg viewBox="0 0 24 24">
                                        {currentStatus === 'running' ? (
                                            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z" />
                                        ) : currentStatus === 'error' || currentStatus === 'missing_token' ? (
                                            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z" />
                                        ) : (
                                            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8zm-1-13h2v6h-2zm0 8h2v2h-2z" />
                                        )}
                                    </svg>
                                </div>
                                <div className="clawbot-status-title">{t('clawBot.runStatus')}</div>
                            </div>
                            <div className={`clawbot-status-pill ${currentStatus}`}>{statusLabel}</div>
                        </div>

                        <div className="clawbot-status-meta">
                            <div className="clawbot-status-meta-item">
                                <span
                                    className={`clawbot-status-meta-dot ${settings?.polling ? 'active' : 'inactive'}`}
                                />
                                {settings?.polling ? t('clawBot.backgroundPolling') : t('clawBot.notPolling')}
                            </div>
                            {settings?.prompt_name && (
                                <div className="clawbot-status-meta-item">
                                    <span className="clawbot-status-meta-dot active" />
                                    {t('clawBot.bindPersona')}：{settings.prompt_name}
                                </div>
                            )}
                            {settings?.ilink_user_id && (
                                <div className="clawbot-status-meta-item">
                                    <span className="clawbot-status-meta-dot active" />
                                    {settings.ilink_user_id}
                                </div>
                            )}
                        </div>

                        {settings?.last_error && <div className="clawbot-status-error">{settings.last_error}</div>}
                    </div>

                    {/* ── 基础设置 ── */}
                    <div className="clawbot-section">
                        <div className="clawbot-section-title">{t('settings.globalSettings')}</div>
                        <div className="clawbot-section-card">
                            <div className="settings-group">
                                <label className="settings-label">{t('clawBot.enableChannel')}</label>
                                <div className="modal-toggle-wrapper">
                                    <label className="toggle-switch">
                                        <input
                                            type="checkbox"
                                            aria-label={t('clawBot.enableChannel')}
                                            checked={form.enabled}
                                            onChange={(event) =>
                                                setForm((current) => ({
                                                    ...current,
                                                    enabled: event.target.checked,
                                                }))
                                            }
                                            disabled={saving}
                                        />
                                        <span className="toggle-slider"></span>
                                    </label>
                                    <span className="toggle-label">
                                        {form.enabled ? t('common.enabled') : t('common.disabled')}
                                    </span>
                                </div>
                                <p className="prompt-modal-hint memory-toggle-hint">{t('clawBot.chatMemoryOnly')}</p>
                            </div>

                            <div className="settings-group">
                                <label className="settings-label">{t('clawBot.baseUrl')}</label>
                                <input
                                    className="settings-input"
                                    aria-label={t('clawBot.baseUrl')}
                                    value={form.base_url}
                                    onChange={(event) =>
                                        setForm((current) => ({ ...current, base_url: event.target.value }))
                                    }
                                    placeholder="https://ilinkai.weixin.qq.com"
                                    disabled={saving}
                                />
                            </div>

                            <div className="settings-group">
                                <label className="settings-label">{t('clawBot.bindPersona')}</label>
                                <CustomSelect
                                    value={form.prompt_id}
                                    options={promptOptions}
                                    onChange={(value) => setForm((current) => ({ ...current, prompt_id: value }))}
                                    ariaLabel={t('clawBot.bindPersona')}
                                    disabled={saving}
                                />
                            </div>
                        </div>
                    </div>

                    {/* ── 命令权限 ── */}
                    <div className="clawbot-section">
                        <div className="clawbot-section-title">{t('clawBot.commandPermissions')}</div>
                        <div className="clawbot-section-card">
                            <div className="settings-group">
                                <div className="clawbot-command-permissions-hint">
                                    {t('clawBot.commandPermissionsHint')}
                                </div>
                                <div className="clawbot-command-permissions">
                                    {commandPermissionItems.map((item) => {
                                        const enabled = form.command_permissions[item.key]
                                        return (
                                            <div className="clawbot-command-row" key={item.key}>
                                                <div className="clawbot-command-copy">
                                                    <div className="clawbot-command-label">{item.label}</div>
                                                    <div className="clawbot-command-desc">{item.description}</div>
                                                </div>
                                                <div className="modal-toggle-wrapper clawbot-command-toggle">
                                                    <label className="toggle-switch">
                                                        <input
                                                            type="checkbox"
                                                            aria-label={item.label}
                                                            checked={enabled}
                                                            onChange={(event) =>
                                                                setForm((current) => ({
                                                                    ...current,
                                                                    command_permissions: {
                                                                        ...current.command_permissions,
                                                                        [item.key]: event.target.checked,
                                                                    },
                                                                }))
                                                            }
                                                            disabled={saving}
                                                        />
                                                        <span className="toggle-slider"></span>
                                                    </label>
                                                    <span className="toggle-label">
                                                        {enabled ? t('common.enabled') : t('common.disabled')}
                                                    </span>
                                                </div>
                                            </div>
                                        )
                                    })}
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* ── 认证凭据 ── */}
                    <div className="clawbot-section">
                        <div className="clawbot-section-title">{t('settings.account')}</div>
                        <div className="clawbot-section-card">
                            <div className="settings-group">
                                <label className="settings-label">{t('clawBot.botToken')}</label>
                                <input
                                    className="settings-input"
                                    type="password"
                                    autoComplete="off"
                                    aria-label={t('clawBot.botToken')}
                                    value={form.bot_token}
                                    onChange={(event) =>
                                        setForm((current) => ({ ...current, bot_token: event.target.value }))
                                    }
                                    placeholder={
                                        settings?.has_bot_token
                                            ? t('clawBot.tokenKeepHint')
                                            : t('clawBot.tokenPlaceholder')
                                    }
                                    disabled={saving}
                                />
                                <div className="clawbot-token-row">
                                    <span className="clawbot-token-status">
                                        {settings?.has_bot_token
                                            ? t('clawBot.savedToken', { token: settings.bot_token || '****' })
                                            : t('clawBot.currentNotSavedToken')}
                                    </span>
                                    {settings?.has_bot_token && (
                                        <button
                                            type="button"
                                            className={`clawbot-inline-btn${form.clear_bot_token ? ' danger' : ''}`}
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
                    </div>

                    {/* ── 操作按钮 ── */}
                    <div className="clawbot-actions">
                        <button
                            type="button"
                            className="clawbot-btn-secondary"
                            onClick={handleStartQRCode}
                            disabled={qrLoading || saving}
                        >
                            {qrLoading ? t('clawBot.getting') : t('clawBot.scanToGet')}
                        </button>
                        <button type="button" className="save-button" onClick={handleSave} disabled={saving}>
                            {saving ? t('common.saving') : t('clawBot.saveSettings')}
                        </button>
                    </div>

                    {/* ── 二维码扫码区 ── */}
                    {qrData && (
                        <div className="clawbot-qrcode-card">
                            <div className="clawbot-qrcode-header">
                                <div className="clawbot-qrcode-title">{t('clawBot.scanLogin')}</div>
                                <div className="clawbot-qrcode-status" role="status">
                                    {qrStatusLabels[qrStatus] || qrStatus}
                                </div>
                            </div>
                            <div className="clawbot-qrcode-body">
                                {qrDisplaySrc ? (
                                    <img className="clawbot-qrcode-image" src={qrDisplaySrc} alt="ClawBot QR Code" />
                                ) : qrData.qrcode_img_content || qrData.qrcode ? (
                                    <div className="clawbot-qrcode-loading">{t('clawBot.qrGenerating')}</div>
                                ) : (
                                    <textarea
                                        className="settings-textarea clawbot-qrcode-text"
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
                                    <label className="settings-label" htmlFor="clawbot-verify-code">
                                        {t('clawBot.qrVerifyCode')}
                                    </label>
                                    <input
                                        id="clawbot-verify-code"
                                        className="settings-input"
                                        value={verifyCode}
                                        onChange={(event) => setVerifyCode(event.target.value)}
                                        autoComplete="one-time-code"
                                        disabled={qrPolling}
                                    />
                                    <button
                                        className="clawbot-btn-secondary"
                                        type="submit"
                                        disabled={qrPolling || !verifyCode.trim()}
                                    >
                                        {t('clawBot.qrSubmitCode')}
                                    </button>
                                </form>
                            )}
                            {qrRetry && (
                                <div className="clawbot-qrcode-hint" role="alert">
                                    <p>{t('clawBot.qrRetryHint')}</p>
                                    <button
                                        type="button"
                                        className="clawbot-btn-secondary"
                                        onClick={() => retryQRCode()}
                                        disabled={qrPolling}
                                    >
                                        {t('clawBot.qrRetry')}
                                    </button>
                                </div>
                            )}
                            <div className="clawbot-qrcode-hint">{t('clawBot.qrCodeHint')}</div>
                        </div>
                    )}
                </div>
            )}
        </motion.div>
    )
}

export default ClawBotSettingsPanel
