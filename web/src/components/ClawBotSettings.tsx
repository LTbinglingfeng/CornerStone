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

    useEffect(() => {
        void loadData()
        return () => {
            if (pollTimerRef.current != null) {
                window.clearTimeout(pollTimerRef.current)
            }
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
        setLoading(true)
        try {
            const [nextSettings, promptList] = await Promise.all([clawBotService.getSettings(), getPrompts()])
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
            const message = error instanceof Error ? error.message : t('common.loadFailed')
            showToast(message, 'error')
        } finally {
            setLoading(false)
        }
    }

    const syncSettings = (nextSettings: ClawBotSettings) => {
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

    const scheduleQRCodePoll = (sessionID: string) => {
        if (pollTimerRef.current != null) {
            window.clearTimeout(pollTimerRef.current)
        }

        pollTimerRef.current = window.setTimeout(async () => {
            try {
                const result = await clawBotService.pollQRCode(sessionID)
                setQRStatus(result.status)

                if (result.status === 'confirmed' && result.settings) {
                    syncSettings(result.settings)
                    setQRData(null)
                    showToast(t('clawBot.scanLogin'), 'success')
                    return
                }

                if (result.status === 'expired') {
                    showToast(t('service.getQRCodeFailed'), 'error')
                    return
                }

                scheduleQRCodePoll(sessionID)
            } catch (error) {
                const message = error instanceof Error ? error.message : t('service.pollQRCodeFailed')
                showToast(message, 'error')
            }
        }, 2500)
    }

    const handleSave = async () => {
        if (saving) return
        setSaving(true)
        try {
            const nextSettings = await clawBotService.updateSettings({
                enabled: form.enabled,
                base_url: form.base_url.trim(),
                bot_token: form.bot_token.trim() || undefined,
                prompt_id: form.prompt_id || undefined,
                clear_bot_token: form.clear_bot_token,
                command_permissions: form.command_permissions,
            })
            syncSettings(nextSettings)
            showToast(t('clawBot.saved'), 'success')
        } catch (error) {
            const message = error instanceof Error ? error.message : t('common.saveFailed')
            showToast(message, 'error')
        } finally {
            setSaving(false)
        }
    }

    const handleStartQRCode = async () => {
        if (qrLoading) return
        setQRLoading(true)
        try {
            const nextSettings = await clawBotService.updateSettings({
                enabled: form.enabled,
                base_url: form.base_url.trim(),
                prompt_id: form.prompt_id || undefined,
                clear_bot_token: false,
                command_permissions: form.command_permissions,
            })
            syncSettings(nextSettings)

            const result = await clawBotService.startQRCode(form.base_url.trim())
            setQRData(result)
            setQRStatus('wait')
            scheduleQRCodePoll(result.session_id)
        } catch (error) {
            const message = error instanceof Error ? error.message : t('service.getQRCodeFailed')
            showToast(message, 'error')
        } finally {
            setQRLoading(false)
        }
    }

    const currentStatus = settings?.status || 'disabled'
    const statusLabelMap: Record<string, string> = {
        disabled: t('clawBot.disabled'),
        missing_token: t('clawBot.missingToken'),
        running: t('clawBot.running'),
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
                <button className="back-button" onClick={onBack}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('clawBot.title')}</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
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
                                <div className="clawbot-qrcode-status">{qrStatus || 'wait'}</div>
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
                                        value={qrData.qrcode_img_content || qrData.qrcode}
                                    />
                                )}
                            </div>
                            <div className="clawbot-qrcode-hint">{t('clawBot.qrCodeHint')}</div>
                        </div>
                    )}
                </div>
            )}
        </motion.div>
    )
}

export default ClawBotSettingsPanel
