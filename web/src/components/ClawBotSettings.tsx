import { useEffect, useMemo, useRef, useState } from 'react'
import { motion } from 'motion/react'
import QRCode from 'qrcode'
import { getPrompts } from '../services/api'
import { clawBotService, type ClawBotQRCodeStartResponse, type ClawBotSettings } from '../services/clawbotService'
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
}

const statusLabelMap: Record<string, string> = {
    disabled: '未启用',
    missing_token: '缺少 Bot Token',
    running: '运行中',
    error: '异常',
    stopped: '已停止',
}

const ClawBotSettingsPanel: React.FC<ClawBotSettingsProps> = ({ onBack }) => {
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
            { value: '', label: '不绑定人设' },
            ...prompts.map((prompt) => ({ value: prompt.id, label: prompt.name })),
        ],
        [prompts]
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
            })
        } catch (error) {
            const message = error instanceof Error ? error.message : '加载失败'
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
                    showToast('扫码登录成功', 'success')
                    return
                }

                if (result.status === 'expired') {
                    showToast('二维码已过期，请重新获取', 'error')
                    return
                }

                scheduleQRCodePoll(sessionID)
            } catch (error) {
                const message = error instanceof Error ? error.message : '轮询失败'
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
            })
            syncSettings(nextSettings)
            showToast('ClawBot 设置已保存', 'success')
        } catch (error) {
            const message = error instanceof Error ? error.message : '保存失败'
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
            })
            syncSettings(nextSettings)

            const result = await clawBotService.startQRCode(form.base_url.trim())
            setQRData(result)
            setQRStatus('wait')
            scheduleQRCodePoll(result.session_id)
        } catch (error) {
            const message = error instanceof Error ? error.message : '获取二维码失败'
            showToast(message, 'error')
        } finally {
            setQRLoading(false)
        }
    }

    const currentStatus = settings?.status || 'disabled'
    const statusLabel = statusLabelMap[currentStatus] || currentStatus || '未配置'

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
                <div className="provider-settings-title">微信 ClawBot</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">加载中...</div>
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
                                <div className="clawbot-status-title">运行状态</div>
                            </div>
                            <div className={`clawbot-status-pill ${currentStatus}`}>{statusLabel}</div>
                        </div>

                        <div className="clawbot-status-meta">
                            <div className="clawbot-status-meta-item">
                                <span
                                    className={`clawbot-status-meta-dot ${settings?.polling ? 'active' : 'inactive'}`}
                                />
                                {settings?.polling ? '后台轮询中' : '当前未轮询'}
                            </div>
                            {settings?.prompt_name && (
                                <div className="clawbot-status-meta-item">
                                    <span className="clawbot-status-meta-dot active" />
                                    人设：{settings.prompt_name}
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
                        <div className="clawbot-section-title">基础设置</div>
                        <div className="clawbot-section-card">
                            <div className="settings-group">
                                <label className="settings-label">启用渠道</label>
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
                                    <span className="toggle-label">{form.enabled ? '已开启' : '已关闭'}</span>
                                </div>
                                <p className="prompt-modal-hint memory-toggle-hint">
                                    聊天仅保存在内存，不会写入聊天记录文件
                                </p>
                            </div>

                            <div className="settings-group">
                                <label className="settings-label">Base URL</label>
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
                                <label className="settings-label">绑定人设</label>
                                <CustomSelect
                                    value={form.prompt_id}
                                    options={promptOptions}
                                    onChange={(value) => setForm((current) => ({ ...current, prompt_id: value }))}
                                    ariaLabel="选择 ClawBot 人设"
                                    disabled={saving}
                                />
                            </div>
                        </div>
                    </div>

                    {/* ── 认证凭据 ── */}
                    <div className="clawbot-section">
                        <div className="clawbot-section-title">认证凭据</div>
                        <div className="clawbot-section-card">
                            <div className="settings-group">
                                <label className="settings-label">Bot Token</label>
                                <input
                                    className="settings-input"
                                    value={form.bot_token}
                                    onChange={(event) =>
                                        setForm((current) => ({ ...current, bot_token: event.target.value }))
                                    }
                                    placeholder={settings?.has_bot_token ? '留空则保留已保存 token' : '输入 Bot Token'}
                                    disabled={saving}
                                />
                                <div className="clawbot-token-row">
                                    <span className="clawbot-token-status">
                                        {settings?.has_bot_token
                                            ? `已保存：${settings.bot_token || '****'}`
                                            : '当前未保存 Bot Token'}
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
                                            {form.clear_bot_token ? '已标记清空' : '清空 Token'}
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
                            {qrLoading ? '获取中...' : '扫码获取'}
                        </button>
                        <button type="button" className="save-button" onClick={handleSave} disabled={saving}>
                            {saving ? '保存中...' : '保存设置'}
                        </button>
                    </div>

                    {/* ── 二维码扫码区 ── */}
                    {qrData && (
                        <div className="clawbot-qrcode-card">
                            <div className="clawbot-qrcode-header">
                                <div className="clawbot-qrcode-title">扫码登录</div>
                                <div className="clawbot-qrcode-status">{qrStatus || 'wait'}</div>
                            </div>
                            <div className="clawbot-qrcode-body">
                                {qrDisplaySrc ? (
                                    <img className="clawbot-qrcode-image" src={qrDisplaySrc} alt="ClawBot QR Code" />
                                ) : qrData.qrcode_img_content || qrData.qrcode ? (
                                    <div className="clawbot-qrcode-loading">二维码生成中...</div>
                                ) : (
                                    <textarea
                                        className="settings-textarea clawbot-qrcode-text"
                                        readOnly
                                        value={qrData.qrcode_img_content || qrData.qrcode}
                                    />
                                )}
                            </div>
                            <div className="clawbot-qrcode-hint">
                                状态值按官方接口原样展示，`scaned` 表示已扫码待确认。
                                {qrDisplaySrc ? ' 当前展示的是按接口返回内容本地生成的二维码。' : ''}
                            </div>
                        </div>
                    )}
                </div>
            )}
        </motion.div>
    )
}

export default ClawBotSettingsPanel
