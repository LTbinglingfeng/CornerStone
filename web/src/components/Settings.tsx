import { useState, useEffect } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { getProviders, updateSystemPrompt } from '../services/api'
import {
    memoryService,
    type MemoryExtractionPromptTemplate,
    type MemoryExtractionSettings,
} from '../services/memoryService'
import { ttsService, type TTSProviderConfig } from '../services/ttsService'
import type { Provider } from '../types/chat'
import {
    getReplyWaitWindowConfig,
    setReplyWaitWindowConfig,
    formatReplyWaitWindowConfig,
    type ReplyWaitWindowConfig,
} from '../utils/replyWaitWindow'
import { NumericInput } from './NumericInput'
import ProviderSettings from './ProviderSettings'
import MemoryProviderSettings from './MemoryProviderSettings'
import ImageProviderSettings from './ImageProviderSettings'
import { useToast } from '../contexts/ToastContext'
import {
    getNotificationsEnabled,
    isNotificationSupported,
    requestNotificationPermission,
    setNotificationsEnabled,
} from '../utils/notifications'
import { centerModalVariants, overlayVariants } from '../utils/motion'
import './Settings.css'

interface SettingsProps {
    onBack: () => void
}

const Settings: React.FC<SettingsProps> = ({ onBack }) => {
    const { showToast } = useToast()
    const [systemPrompt, setSystemPrompt] = useState('')
    const [editingPrompt, setEditingPrompt] = useState('')
    const [activeProviderName, setActiveProviderName] = useState('')
    const [imageProviderPreview, setImageProviderPreview] = useState<{ title: string; detail: string }>({
        title: '未配置',
        detail: '',
    })
    const [memoryProvider, setMemoryProvider] = useState<Provider | null>(null)
    const [memoryEnabled, setMemoryEnabled] = useState(false)
    const [ttsEnabled, setTTSEnabledState] = useState(false)
    const [ttsProvider, setTTSProvider] = useState<TTSProviderConfig | null>(null)
    const [showTTSProviderModal, setShowTTSProviderModal] = useState(false)
    const [editingTTSProvider, setEditingTTSProvider] = useState<TTSProviderConfig>(() => ({
        type: 'minimax',
        base_url: 'https://api.minimaxi.com',
        api_key: '',
        model: 'speech-2.6-hd',
        voice_setting: { voice_id: 'male-qn-qingse', speed: 1 },
        language_boost: '',
    }))
    const [memoryExtractionSettings, setMemoryExtractionSettings] = useState<MemoryExtractionSettings | null>(null)
    const [memoryExtractionRounds, setMemoryExtractionRounds] = useState(5)
    const [memoryExtractionMaxRounds, setMemoryExtractionMaxRounds] = useState(64)
    const [memoryExtractionProviderName, setMemoryExtractionProviderName] = useState('')
    const [showMemoryExtractionRoundsModal, setShowMemoryExtractionRoundsModal] = useState(false)
    const [editingMemoryExtractionRounds, setEditingMemoryExtractionRounds] = useState(5)
    const [savingMemoryExtractionRounds, setSavingMemoryExtractionRounds] = useState(false)
    const [memoryRefreshInterval, setMemoryRefreshInterval] = useState(5)
    const [memoryRefreshMaxInterval, setMemoryRefreshMaxInterval] = useState(99)
    const [showMemoryRefreshIntervalModal, setShowMemoryRefreshIntervalModal] = useState(false)
    const [editingMemoryRefreshInterval, setEditingMemoryRefreshInterval] = useState(5)
    const [savingMemoryRefreshInterval, setSavingMemoryRefreshInterval] = useState(false)
    const [showMemoryExtractionPromptModal, setShowMemoryExtractionPromptModal] = useState(false)
    const [loadingMemoryExtractionPrompt, setLoadingMemoryExtractionPrompt] = useState(false)
    const [savingMemoryExtractionPrompt, setSavingMemoryExtractionPrompt] = useState(false)
    const [editingMemoryExtractionPrompt, setEditingMemoryExtractionPrompt] = useState('')
    const [defaultMemoryExtractionPrompt, setDefaultMemoryExtractionPrompt] = useState('')
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [showProviderSettings, setShowProviderSettings] = useState(false)
    const [showImageProviderSettings, setShowImageProviderSettings] = useState(false)
    const [showMemoryProviderSettings, setShowMemoryProviderSettings] = useState(false)
    const [showPromptModal, setShowPromptModal] = useState(false)
    const [replyWaitConfig, setReplyWaitConfigState] = useState<ReplyWaitWindowConfig>(() => getReplyWaitWindowConfig())
    const [editingReplyWaitConfig, setEditingReplyWaitConfig] = useState<ReplyWaitWindowConfig>(() =>
        getReplyWaitWindowConfig()
    )
    const [showReplyWaitModal, setShowReplyWaitModal] = useState(false)
    const [notificationsEnabled, setNotificationsEnabledState] = useState(() => getNotificationsEnabled())
    const [notificationsSupported, setNotificationsSupported] = useState(() => isNotificationSupported())
    const [notificationPermission, setNotificationPermission] = useState<NotificationPermission | 'unsupported'>(() =>
        isNotificationSupported() ? Notification.permission : 'unsupported'
    )

    useEffect(() => {
        loadData()
    }, [])

    useEffect(() => {
        const supported = isNotificationSupported()
        setNotificationsSupported(supported)
        setNotificationPermission(supported ? Notification.permission : 'unsupported')
        setNotificationsEnabledState(getNotificationsEnabled())
    }, [])

    const loadData = async ({ showLoading = true }: { showLoading?: boolean } = {}) => {
        if (showLoading) setLoading(true)
        const providersData = await getProviders()
        if (providersData) {
            setSystemPrompt(providersData.system_prompt)
            const activeProvider = providersData.providers.find((p) => p.id === providersData.active_provider_id)
            setActiveProviderName(activeProvider?.name || '未设置')
            const configuredImageProviderId = providersData.image_provider_id || ''
            const imageProviders = providersData.providers.filter((p) => p.type === 'gemini_image')
            if (configuredImageProviderId) {
                const selected = imageProviders.find((p) => p.id === configuredImageProviderId)
                setImageProviderPreview({
                    title: selected?.name || '未配置',
                    detail: selected?.model || '',
                })
            } else {
                const auto = imageProviders[0]
                if (auto) {
                    setImageProviderPreview({
                        title: '自动选择',
                        detail: `${auto.name || auto.id}${auto.model ? ` · ${auto.model}` : ''}`,
                    })
                } else {
                    setImageProviderPreview({ title: '未配置', detail: '暂无生图供应商' })
                }
            }
            setMemoryProvider(providersData.memory_provider || null)
            setMemoryEnabled(!!providersData.memory_enabled)
        }
        try {
            const settings = await ttsService.getTTSSettings()
            setTTSEnabledState(settings.enabled)
            setTTSProvider(settings.provider)
        } catch {
            setTTSEnabledState(false)
            setTTSProvider(null)
        }
        try {
            const settings = await memoryService.getMemoryExtractionSettings()
            setMemoryExtractionSettings(settings)
            setMemoryExtractionRounds(settings.rounds)
            setMemoryExtractionMaxRounds(settings.max_rounds)
            setMemoryExtractionProviderName(settings.provider_name || '')
            setMemoryRefreshInterval(settings.refresh_interval)
            setMemoryRefreshMaxInterval(settings.max_refresh_interval)
        } catch {
            setMemoryExtractionSettings(null)
        }
        if (showLoading) setLoading(false)
    }

    const setReplyWaitConfig = (config: ReplyWaitWindowConfig) => {
        setReplyWaitWindowConfig(config)
        setReplyWaitConfigState(getReplyWaitWindowConfig())
    }

    const handleOpenPromptModal = () => {
        setEditingPrompt(systemPrompt)
        setShowPromptModal(true)
    }

    const handleClosePromptModal = () => {
        setShowPromptModal(false)
    }

    const handleOpenReplyWaitModal = () => {
        setEditingReplyWaitConfig(replyWaitConfig)
        setShowReplyWaitModal(true)
    }

    const handleCloseReplyWaitModal = () => {
        setShowReplyWaitModal(false)
    }

    const handleOpenMemoryExtractionRoundsModal = () => {
        const rounds = memoryExtractionSettings?.rounds || memoryExtractionRounds || 5
        setEditingMemoryExtractionRounds(rounds)
        setShowMemoryExtractionRoundsModal(true)
    }

    const handleCloseMemoryExtractionRoundsModal = () => {
        setShowMemoryExtractionRoundsModal(false)
    }

    const handleSaveMemoryExtractionRounds = async () => {
        if (savingMemoryExtractionRounds) return
        setSavingMemoryExtractionRounds(true)
        try {
            const settings = await memoryService.setMemoryExtractionRounds(editingMemoryExtractionRounds)
            setMemoryExtractionSettings(settings)
            setMemoryExtractionRounds(settings.rounds)
            setMemoryExtractionMaxRounds(settings.max_rounds)
            setMemoryExtractionProviderName(settings.provider_name || '')
            showToast('记忆提取轮数已保存', 'success')
            handleCloseMemoryExtractionRoundsModal()
        } catch (error) {
            const message = error instanceof Error ? error.message : '保存失败'
            showToast(message, 'error')
        } finally {
            setSavingMemoryExtractionRounds(false)
        }
    }

    const handleOpenMemoryRefreshIntervalModal = () => {
        const interval = memoryExtractionSettings?.refresh_interval || memoryRefreshInterval || 5
        setEditingMemoryRefreshInterval(interval)
        setShowMemoryRefreshIntervalModal(true)
    }

    const handleCloseMemoryRefreshIntervalModal = () => {
        setShowMemoryRefreshIntervalModal(false)
    }

    const handleSaveMemoryRefreshInterval = async () => {
        if (savingMemoryRefreshInterval) return
        setSavingMemoryRefreshInterval(true)
        try {
            const settings = await memoryService.setMemoryRefreshInterval(editingMemoryRefreshInterval)
            setMemoryExtractionSettings(settings)
            setMemoryRefreshInterval(settings.refresh_interval)
            setMemoryRefreshMaxInterval(settings.max_refresh_interval)
            showToast('记忆刷新间隔已保存', 'success')
            handleCloseMemoryRefreshIntervalModal()
        } catch (error) {
            const message = error instanceof Error ? error.message : '保存失败'
            showToast(message, 'error')
        } finally {
            setSavingMemoryRefreshInterval(false)
        }
    }

    const handleOpenMemoryExtractionPromptModal = () => {
        setShowMemoryExtractionPromptModal(true)
        setLoadingMemoryExtractionPrompt(true)
        setSavingMemoryExtractionPrompt(false)
        setEditingMemoryExtractionPrompt('')
        setDefaultMemoryExtractionPrompt('')

        void (async () => {
            try {
                const data: MemoryExtractionPromptTemplate = await memoryService.getMemoryExtractionPromptTemplate()
                setEditingMemoryExtractionPrompt(data.template || '')
                setDefaultMemoryExtractionPrompt(data.default_template || '')
            } catch (error) {
                const message = error instanceof Error ? error.message : '加载失败'
                showToast(message, 'error')
                setShowMemoryExtractionPromptModal(false)
            } finally {
                setLoadingMemoryExtractionPrompt(false)
            }
        })()
    }

    const handleCloseMemoryExtractionPromptModal = () => {
        setShowMemoryExtractionPromptModal(false)
    }

    const handleSaveMemoryExtractionPrompt = async () => {
        if (savingMemoryExtractionPrompt || loadingMemoryExtractionPrompt) return
        setSavingMemoryExtractionPrompt(true)
        try {
            await memoryService.updateMemoryExtractionPromptTemplate(editingMemoryExtractionPrompt)
            showToast('记忆提取提示词已保存', 'success')
            handleCloseMemoryExtractionPromptModal()
        } catch (error) {
            const message = error instanceof Error ? error.message : '保存失败'
            showToast(message, 'error')
        } finally {
            setSavingMemoryExtractionPrompt(false)
        }
    }

    const handleSaveReplyWaitConfig = () => {
        setReplyWaitConfig(editingReplyWaitConfig)
        showToast('回复等候窗口已保存', 'success')
        handleCloseReplyWaitModal()
    }

    const handleSaveSystemPrompt = async () => {
        setSaving(true)
        const success = await updateSystemPrompt(editingPrompt)
        if (success) {
            setSystemPrompt(editingPrompt)
            showToast('系统提示词已保存', 'success')
            handleClosePromptModal()
        } else {
            showToast('保存失败', 'error')
        }
        setSaving(false)
    }

    const handleNotificationsToggle = async (enabled: boolean) => {
        if (!enabled) {
            setNotificationsEnabled(false)
            setNotificationsEnabledState(false)
            showToast('已关闭系统通知', 'success')
            return
        }

        if (!isNotificationSupported()) {
            setNotificationsSupported(false)
            setNotificationPermission('unsupported')
            setNotificationsEnabled(false)
            setNotificationsEnabledState(false)
            showToast('当前浏览器不支持系统通知', 'error')
            return
        }

        if (typeof window !== 'undefined' && window.isSecureContext === false && location.hostname !== 'localhost') {
            showToast('系统通知需要 HTTPS 环境', 'error')
            return
        }

        const permission = await requestNotificationPermission()
        setNotificationPermission(permission)
        if (permission !== 'granted') {
            setNotificationsEnabled(false)
            setNotificationsEnabledState(false)
            showToast(permission === 'denied' ? '已拒绝通知权限，请在浏览器设置中开启' : '请允许通知权限', 'error')
            return
        }

        setNotificationsEnabled(true)
        setNotificationsEnabledState(true)
        showToast('已开启系统通知', 'success')
    }

    const handleTTSEnabledChange = async (enabled: boolean) => {
        if (saving) return
        setSaving(true)
        try {
            const settings = await ttsService.updateTTSSettings({ enabled })
            setTTSEnabledState(settings.enabled)
            setTTSProvider(settings.provider)
            showToast(enabled ? '已开启 TTS' : '已关闭 TTS', 'success')
        } catch (error) {
            console.error('Failed to set tts enabled:', error)
            showToast('设置失败', 'error')
        } finally {
            setSaving(false)
        }
    }

    const handleOpenTTSProviderModal = () => {
        const current = ttsProvider
        if (current) {
            setEditingTTSProvider({
                type: current.type || 'minimax',
                base_url: current.base_url || 'https://api.minimaxi.com',
                api_key: current.api_key || '',
                model: current.model || 'speech-2.6-hd',
                voice_setting: {
                    voice_id: current.voice_setting?.voice_id || 'male-qn-qingse',
                    speed: current.voice_setting?.speed ?? 1,
                },
                language_boost: current.language_boost || '',
            })
        } else {
            setEditingTTSProvider({
                type: 'minimax',
                base_url: 'https://api.minimaxi.com',
                api_key: '',
                model: 'speech-2.6-hd',
                voice_setting: { voice_id: 'male-qn-qingse', speed: 1 },
                language_boost: '',
            })
        }
        setShowTTSProviderModal(true)
    }

    const handleCloseTTSProviderModal = () => {
        setShowTTSProviderModal(false)
    }

    const handleSaveTTSProvider = async () => {
        if (saving) return
        setSaving(true)
        try {
            const languageBoost = (editingTTSProvider.language_boost || '').trim()
            const provider: TTSProviderConfig = {
                ...editingTTSProvider,
                base_url: editingTTSProvider.base_url.trim(),
                api_key: editingTTSProvider.api_key.trim(),
                model: editingTTSProvider.model.trim(),
                voice_setting: {
                    voice_id: editingTTSProvider.voice_setting.voice_id.trim(),
                    speed: editingTTSProvider.voice_setting.speed,
                },
                ...(languageBoost ? { language_boost: languageBoost } : {}),
            }
            const settings = await ttsService.updateTTSSettings({ provider })
            setTTSEnabledState(settings.enabled)
            setTTSProvider(settings.provider)
            showToast('已保存 TTS 提供商', 'success')
            handleCloseTTSProviderModal()
        } catch (error) {
            console.error('Failed to save tts provider:', error)
            showToast('保存失败', 'error')
        } finally {
            setSaving(false)
        }
    }

    const handleMemoryEnabledChange = async (enabled: boolean) => {
        if (saving) return
        setSaving(true)
        try {
            await memoryService.setMemoryEnabled(enabled)
            setMemoryEnabled(enabled)
            showToast(enabled ? '已开启长期记忆' : '已关闭长期记忆', 'success')
        } catch (error) {
            console.error('Failed to set memory enabled:', error)
            showToast('设置失败', 'error')
        } finally {
            setSaving(false)
        }
    }

    const handleProviderSettingsBack = () => {
        setShowProviderSettings(false)
    }

    const handleImageProviderSettingsBack = () => {
        setShowImageProviderSettings(false)
    }

    const handleMemoryProviderSettingsBack = () => {
        setShowMemoryProviderSettings(false)
    }

    const getPromptPreview = () => {
        if (!systemPrompt) return '未设置'
        if (systemPrompt.length <= 20) return systemPrompt
        return systemPrompt.substring(0, 20) + '...'
    }

    const getMemoryProviderPreview = () => {
        if (memoryProvider) {
            const name = memoryProvider.name || '未命名'
            const model = memoryProvider.model || '未设置模型'
            return { title: name, detail: model }
        }
        if (activeProviderName) return { title: '跟随对话模型', detail: activeProviderName }
        return { title: '跟随对话模型', detail: '默认' }
    }

    const getTTSProviderPreview = () => {
        if (!ttsProvider) return { title: '未配置', detail: '' }
        const model = ttsProvider.model || ''
        const voiceId = ttsProvider.voice_setting?.voice_id || ''
        const detail = [model, voiceId].filter(Boolean).join(' · ')
        return { title: 'MiniMax', detail }
    }

    const getReplyWaitPreview = () => {
        return formatReplyWaitWindowConfig(replyWaitConfig)
    }

    const getMemoryExtractionRoundsPreview = () => {
        const rounds = memoryExtractionRounds || memoryExtractionSettings?.rounds || 5
        return `${rounds}轮`
    }

    const getMemoryExtractionRoundsDetail = () => {
        const maxRounds = memoryExtractionMaxRounds || memoryExtractionSettings?.max_rounds || 1
        const providerLabel = memoryExtractionProviderName ? `（上限来自：${memoryExtractionProviderName}）` : ''
        return `最多${maxRounds}轮${providerLabel}`
    }

    const getMemoryRefreshIntervalPreview = () => {
        const interval = memoryRefreshInterval || memoryExtractionSettings?.refresh_interval || 5
        return `${interval}轮`
    }

    const getMemoryRefreshIntervalDetail = () => {
        const maxInterval = memoryRefreshMaxInterval || memoryExtractionSettings?.max_refresh_interval || 99
        return `最多${maxInterval}轮`
    }

    const memoryProviderPreview = getMemoryProviderPreview()
    const ttsProviderPreview = getTTSProviderPreview()

    return (
        <div className="settings">
            <div className="settings-header">
                <button className="back-button" onClick={onBack}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="settings-title">设置</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="settings-loading">加载中...</div>
            ) : (
                <div className="settings-content">
                    {/* 供应商设置入口 */}
                    <div className="settings-section">
                        <h3>供应商</h3>
                        <button className="settings-entry-btn" onClick={() => setShowProviderSettings(true)}>
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">当前供应商</span>
                                <span className="settings-entry-value">{activeProviderName}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>

                        <button
                            className="settings-entry-btn"
                            onClick={() => setShowImageProviderSettings(true)}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">生图供应商</span>
                                <span className="settings-entry-value">{imageProviderPreview.title}</span>
                                {imageProviderPreview.detail && (
                                    <span className="settings-entry-subvalue">{imageProviderPreview.detail}</span>
                                )}
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                    </div>

                    {/* 全局设置 */}
                    <div className="settings-section">
                        <h3>全局设置</h3>
                        <button className="settings-entry-btn" onClick={handleOpenPromptModal}>
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">默认系统提示词</span>
                                <span className="settings-entry-value">{getPromptPreview()}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenReplyWaitModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">回复等候窗口</span>
                                <span className="settings-entry-value">{getReplyWaitPreview()}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>

                        <div className="settings-group" style={{ marginTop: 12 }}>
                            <label className="settings-label">系统通知</label>
                            <div className="modal-toggle-wrapper">
                                <label className="toggle-switch">
                                    <input
                                        type="checkbox"
                                        checked={notificationsEnabled}
                                        onChange={(e) => void handleNotificationsToggle(e.target.checked)}
                                        disabled={saving || !notificationsSupported}
                                    />
                                    <span className="toggle-slider"></span>
                                </label>
                                <span className="toggle-label">
                                    {notificationsSupported
                                        ? notificationsEnabled
                                            ? '开启'
                                            : notificationPermission === 'denied'
                                              ? '已拒绝'
                                              : '关闭'
                                        : '不支持'}
                                </span>
                            </div>
                            <p className="prompt-modal-hint memory-toggle-hint">仅在不在聊天详情界面时提醒</p>
                        </div>
                    </div>

                    {/* 语音设置 */}
                    <div className="settings-section">
                        <h3>语音</h3>

                        <div className="settings-group">
                            <label className="settings-label">TTS</label>
                            <div className="modal-toggle-wrapper">
                                <label className="toggle-switch">
                                    <input
                                        type="checkbox"
                                        checked={ttsEnabled}
                                        onChange={(e) => handleTTSEnabledChange(e.target.checked)}
                                        disabled={saving}
                                    />
                                    <span className="toggle-slider"></span>
                                </label>
                                <span className="toggle-label">{ttsEnabled ? '开启' : '关闭'}</span>
                            </div>
                            <p className="prompt-modal-hint memory-toggle-hint">开启后会为 AI 回复生成语音按钮</p>
                        </div>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenTTSProviderModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">TTS 提供商</span>
                                <span className="settings-entry-value">{ttsProviderPreview.title}</span>
                                {ttsProviderPreview.detail && (
                                    <span className="settings-entry-subvalue">{ttsProviderPreview.detail}</span>
                                )}
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                    </div>

                    {/* 长期记忆设置 */}
                    <div className="settings-section">
                        <h3>长期记忆</h3>

                        <p className="prompt-modal-hint">
                            提示：开启后会将最近 {memoryExtractionRounds || 5}{' '}
                            轮对话片段发送给记忆处理模型用于提取，请勿输入敏感信息。
                        </p>

                        <div className="settings-group">
                            <label className="settings-label">记忆功能</label>
                            <div className="modal-toggle-wrapper">
                                <label className="toggle-switch">
                                    <input
                                        type="checkbox"
                                        checked={memoryEnabled}
                                        onChange={(e) => handleMemoryEnabledChange(e.target.checked)}
                                        disabled={saving}
                                    />
                                    <span className="toggle-slider"></span>
                                </label>
                                <span className="toggle-label">{memoryEnabled ? '开启' : '关闭'}</span>
                            </div>
                            <p className="prompt-modal-hint memory-toggle-hint">关闭后将不会提取和使用记忆</p>
                        </div>

                        <button
                            className="settings-entry-btn"
                            onClick={() => setShowMemoryProviderSettings(true)}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">记忆提供商</span>
                                <span className="settings-entry-value">{memoryProviderPreview.title}</span>
                                <span className="settings-entry-subvalue">{memoryProviderPreview.detail}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                        <p className="prompt-modal-hint memory-provider-hint">
                            用于提取和处理长期记忆，建议选择快速便宜的模型
                        </p>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenMemoryExtractionRoundsModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">记忆提取轮数</span>
                                <span className="settings-entry-value">{getMemoryExtractionRoundsPreview()}</span>
                                <span className="settings-entry-subvalue">{getMemoryExtractionRoundsDetail()}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                        <p className="prompt-modal-hint memory-provider-hint">
                            每轮包含用户与 AI 各一条消息，数值越大提取越完整，但也更耗时
                        </p>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenMemoryRefreshIntervalModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">记忆刷新间隔</span>
                                <span className="settings-entry-value">{getMemoryRefreshIntervalPreview()}</span>
                                <span className="settings-entry-subvalue">{getMemoryRefreshIntervalDetail()}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                        <p className="prompt-modal-hint memory-provider-hint">每隔多少轮对话触发一次记忆提取</p>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenMemoryExtractionPromptModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">记忆提取提示词</span>
                                <span className="settings-entry-value">编辑</span>
                                <span className="settings-entry-subvalue">
                                    支持 &#123;&#123;user&#125;&#125;/&#123;&#123;avatar&#125;&#125;/
                                    &#123;&#123;EXISTING_MEMORIES&#125;&#125;/&#123;&#123;CHAT_CONTENT&#125;&#125;
                                </span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                    </div>
                </div>
            )}

            <AnimatePresence onExitComplete={() => void loadData({ showLoading: false })}>
                {showProviderSettings && <ProviderSettings onBack={handleProviderSettingsBack} />}
            </AnimatePresence>

            <AnimatePresence onExitComplete={() => void loadData({ showLoading: false })}>
                {showImageProviderSettings && <ImageProviderSettings onBack={handleImageProviderSettingsBack} />}
            </AnimatePresence>

            <AnimatePresence onExitComplete={() => void loadData({ showLoading: false })}>
                {showMemoryProviderSettings && <MemoryProviderSettings onBack={handleMemoryProviderSettingsBack} />}
            </AnimatePresence>

            <AnimatePresence>
                {showPromptModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleClosePromptModal}
                    >
                        <motion.div
                            className="prompt-modal-card"
                            initial="hidden"
                            animate="visible"
                            exit="hidden"
                            variants={centerModalVariants}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="prompt-modal-header">
                                <h3>编辑系统提示词</h3>
                                <button className="prompt-modal-close" onClick={handleClosePromptModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">此提示词将作为所有对话的默认全局系统提示词</p>
                                <textarea
                                    className="prompt-modal-textarea"
                                    value={editingPrompt}
                                    onChange={(e) => setEditingPrompt(e.target.value)}
                                    placeholder="输入系统提示词..."
                                    rows={8}
                                />
                            </div>

                            <div className="prompt-modal-footer">
                                <button className="prompt-modal-btn cancel" onClick={handleClosePromptModal}>
                                    取消
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveSystemPrompt}
                                    disabled={saving}
                                >
                                    {saving ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showReplyWaitModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseReplyWaitModal}
                    >
                        <motion.div
                            className="prompt-modal-card"
                            initial="hidden"
                            animate="visible"
                            exit="hidden"
                            variants={centerModalVariants}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="prompt-modal-header">
                                <h3>回复等候窗口</h3>
                                <button className="prompt-modal-close" onClick={handleCloseReplyWaitModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">用于合并你连续发送的多条消息后再让 AI 回复</p>

                                <div className="settings-group">
                                    <label className="settings-label">合并模式</label>
                                    <select
                                        className="settings-input"
                                        value={editingReplyWaitConfig.mode}
                                        onChange={(e) =>
                                            setEditingReplyWaitConfig((prev) => ({
                                                ...prev,
                                                mode: e.target.value as ReplyWaitWindowConfig['mode'],
                                            }))
                                        }
                                    >
                                        <option value="fixed">固定时间</option>
                                        <option value="sliding">滑动时间</option>
                                    </select>
                                </div>

                                <div className="settings-group">
                                    <label className="settings-label">等待秒数</label>
                                    <NumericInput
                                        className="settings-input"
                                        min={0}
                                        max={120}
                                        step={1}
                                        value={editingReplyWaitConfig.seconds}
                                        parseAs="int"
                                        onValueChange={(seconds) =>
                                            setEditingReplyWaitConfig((prev) => ({
                                                ...prev,
                                                seconds,
                                            }))
                                        }
                                    />
                                </div>

                                <p className="prompt-modal-hint">0 秒表示立即发送（不合并）</p>
                            </div>

                            <div className="prompt-modal-footer">
                                <button className="prompt-modal-btn cancel" onClick={handleCloseReplyWaitModal}>
                                    取消
                                </button>
                                <button className="prompt-modal-btn save" onClick={handleSaveReplyWaitConfig}>
                                    保存
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showTTSProviderModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseTTSProviderModal}
                    >
                        <motion.div
                            className="prompt-modal-card"
                            initial="hidden"
                            animate="visible"
                            exit="hidden"
                            variants={centerModalVariants}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="prompt-modal-header">
                                <h3>TTS 提供商</h3>
                                <button className="prompt-modal-close" onClick={handleCloseTTSProviderModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">仅支持 MiniMax，同步生成 mp3 音频</p>

                                <div className="settings-group">
                                    <label className="settings-label">Base URL</label>
                                    <input
                                        className="settings-input"
                                        value={editingTTSProvider.base_url}
                                        onChange={(e) =>
                                            setEditingTTSProvider((prev) => ({ ...prev, base_url: e.target.value }))
                                        }
                                        placeholder="https://api.minimaxi.com"
                                    />
                                </div>

                                <div className="settings-group">
                                    <label className="settings-label">API Key</label>
                                    <input
                                        className="settings-input"
                                        type="password"
                                        value={editingTTSProvider.api_key}
                                        onChange={(e) =>
                                            setEditingTTSProvider((prev) => ({ ...prev, api_key: e.target.value }))
                                        }
                                        placeholder="MiniMax API Key"
                                        autoComplete="off"
                                    />
                                    <p className="prompt-modal-hint">已配置过可保留为 ****，不修改则保持不变</p>
                                </div>

                                <div className="settings-group">
                                    <label className="settings-label">Model</label>
                                    <input
                                        className="settings-input"
                                        value={editingTTSProvider.model}
                                        onChange={(e) =>
                                            setEditingTTSProvider((prev) => ({ ...prev, model: e.target.value }))
                                        }
                                        placeholder="speech-2.6-hd"
                                    />
                                </div>

                                <div className="settings-group">
                                    <label className="settings-label">Voice ID</label>
                                    <input
                                        className="settings-input"
                                        value={editingTTSProvider.voice_setting.voice_id}
                                        onChange={(e) =>
                                            setEditingTTSProvider((prev) => ({
                                                ...prev,
                                                voice_setting: { ...prev.voice_setting, voice_id: e.target.value },
                                            }))
                                        }
                                        placeholder="male-qn-qingse"
                                    />
                                </div>

                                <div className="settings-group">
                                    <label className="settings-label">Speed</label>
                                    <NumericInput
                                        className="settings-input"
                                        min={0.5}
                                        max={2}
                                        step={0.1}
                                        value={editingTTSProvider.voice_setting.speed}
                                        parseAs="float"
                                        onValueChange={(speed) =>
                                            setEditingTTSProvider((prev) => ({
                                                ...prev,
                                                voice_setting: { ...prev.voice_setting, speed },
                                            }))
                                        }
                                    />
                                </div>

                                <div className="settings-group">
                                    <label className="settings-label">Language Boost</label>
                                    <input
                                        className="settings-input"
                                        value={editingTTSProvider.language_boost || ''}
                                        onChange={(e) =>
                                            setEditingTTSProvider((prev) => ({
                                                ...prev,
                                                language_boost: e.target.value,
                                            }))
                                        }
                                        placeholder="auto / Chinese / English ..."
                                    />
                                </div>
                            </div>

                            <div className="prompt-modal-footer">
                                <button
                                    className="prompt-modal-btn cancel"
                                    onClick={handleCloseTTSProviderModal}
                                    disabled={saving}
                                >
                                    取消
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveTTSProvider}
                                    disabled={saving}
                                >
                                    {saving ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showMemoryExtractionRoundsModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseMemoryExtractionRoundsModal}
                    >
                        <motion.div
                            className="prompt-modal-card"
                            initial="hidden"
                            animate="visible"
                            exit="hidden"
                            variants={centerModalVariants}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="prompt-modal-header">
                                <h3>记忆提取轮数</h3>
                                <button className="prompt-modal-close" onClick={handleCloseMemoryExtractionRoundsModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">
                                    用于控制发送给记忆提取模型的最近对话轮数（每轮 = 用户 + AI）
                                </p>
                                <div className="settings-group">
                                    <label className="settings-label">轮数</label>
                                    <NumericInput
                                        className="settings-input"
                                        min={1}
                                        max={memoryExtractionMaxRounds || 1}
                                        step={1}
                                        value={editingMemoryExtractionRounds}
                                        parseAs="int"
                                        onValueChange={(nextRounds) => {
                                            const max = memoryExtractionMaxRounds || 1
                                            setEditingMemoryExtractionRounds(Math.max(1, Math.min(nextRounds, max)))
                                        }}
                                    />
                                </div>
                                <p className="prompt-modal-hint">{getMemoryExtractionRoundsDetail()}</p>
                            </div>

                            <div className="prompt-modal-footer">
                                <button
                                    className="prompt-modal-btn cancel"
                                    onClick={handleCloseMemoryExtractionRoundsModal}
                                >
                                    取消
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveMemoryExtractionRounds}
                                    disabled={savingMemoryExtractionRounds}
                                >
                                    {savingMemoryExtractionRounds ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showMemoryRefreshIntervalModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseMemoryRefreshIntervalModal}
                    >
                        <motion.div
                            className="prompt-modal-card"
                            initial="hidden"
                            animate="visible"
                            exit="hidden"
                            variants={centerModalVariants}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="prompt-modal-header">
                                <h3>记忆刷新间隔</h3>
                                <button className="prompt-modal-close" onClick={handleCloseMemoryRefreshIntervalModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">每隔多少轮对话触发一次记忆提取（每轮 = 用户 + AI）</p>
                                <div className="settings-group">
                                    <label className="settings-label">间隔轮数</label>
                                    <NumericInput
                                        className="settings-input"
                                        min={1}
                                        max={memoryRefreshMaxInterval || 99}
                                        step={1}
                                        value={editingMemoryRefreshInterval}
                                        parseAs="int"
                                        onValueChange={(nextInterval) => {
                                            const max = memoryRefreshMaxInterval || 99
                                            setEditingMemoryRefreshInterval(Math.max(1, Math.min(nextInterval, max)))
                                        }}
                                    />
                                </div>
                                <p className="prompt-modal-hint">{getMemoryRefreshIntervalDetail()}</p>
                            </div>

                            <div className="prompt-modal-footer">
                                <button
                                    className="prompt-modal-btn cancel"
                                    onClick={handleCloseMemoryRefreshIntervalModal}
                                >
                                    取消
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveMemoryRefreshInterval}
                                    disabled={savingMemoryRefreshInterval}
                                >
                                    {savingMemoryRefreshInterval ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showMemoryExtractionPromptModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseMemoryExtractionPromptModal}
                    >
                        <motion.div
                            className="prompt-modal-card"
                            initial="hidden"
                            animate="visible"
                            exit="hidden"
                            variants={centerModalVariants}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="prompt-modal-header">
                                <h3>记忆提取提示词</h3>
                                <button className="prompt-modal-close" onClick={handleCloseMemoryExtractionPromptModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">
                                    必须保留 &#123;&#123;EXISTING_MEMORIES&#125;&#125; 与
                                    &#123;&#123;CHAT_CONTENT&#125;&#125; 占位符。
                                    可用变量：&#123;&#123;user&#125;&#125;（用户） /
                                    &#123;&#123;avatar&#125;&#125;（角色）
                                </p>

                                <div className="memory-extraction-template-actions">
                                    <button
                                        className="action-button edit"
                                        onClick={() => setEditingMemoryExtractionPrompt(defaultMemoryExtractionPrompt)}
                                        disabled={loadingMemoryExtractionPrompt || defaultMemoryExtractionPrompt === ''}
                                        type="button"
                                    >
                                        恢复默认
                                    </button>
                                </div>

                                <textarea
                                    className="prompt-modal-textarea"
                                    value={editingMemoryExtractionPrompt}
                                    onChange={(e) => setEditingMemoryExtractionPrompt(e.target.value)}
                                    placeholder={loadingMemoryExtractionPrompt ? '加载中...' : '输入记忆提取提示词...'}
                                    rows={12}
                                    disabled={loadingMemoryExtractionPrompt}
                                />
                            </div>

                            <div className="prompt-modal-footer">
                                <button
                                    className="prompt-modal-btn cancel"
                                    onClick={handleCloseMemoryExtractionPromptModal}
                                >
                                    取消
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveMemoryExtractionPrompt}
                                    disabled={savingMemoryExtractionPrompt || loadingMemoryExtractionPrompt}
                                >
                                    {savingMemoryExtractionPrompt ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    )
}

export default Settings
