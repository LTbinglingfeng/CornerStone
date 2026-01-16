import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import { getProviders, updateSystemPrompt } from '../services/api'
import { memoryService, type MemoryExtractionPromptTemplate, type MemoryExtractionSettings } from '../services/memoryService'
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
import { useToast } from '../contexts/ToastContext'
import './Settings.css'

interface SettingsProps {
    onBack: () => void
}

const Settings: React.FC<SettingsProps> = ({ onBack }) => {
    const { showToast } = useToast()
    const [systemPrompt, setSystemPrompt] = useState('')
    const [editingPrompt, setEditingPrompt] = useState('')
    const [activeProviderName, setActiveProviderName] = useState('')
    const [memoryProvider, setMemoryProvider] = useState<Provider | null>(null)
    const [memoryEnabled, setMemoryEnabled] = useState(false)
    const [memoryExtractionSettings, setMemoryExtractionSettings] = useState<MemoryExtractionSettings | null>(null)
    const [memoryExtractionRounds, setMemoryExtractionRounds] = useState(5)
    const [memoryExtractionMaxRounds, setMemoryExtractionMaxRounds] = useState(64)
    const [memoryExtractionProviderName, setMemoryExtractionProviderName] = useState('')
    const [showMemoryExtractionRoundsModal, setShowMemoryExtractionRoundsModal] = useState(false)
    const [editingMemoryExtractionRounds, setEditingMemoryExtractionRounds] = useState(5)
    const [savingMemoryExtractionRounds, setSavingMemoryExtractionRounds] = useState(false)
    const [showMemoryExtractionPromptModal, setShowMemoryExtractionPromptModal] = useState(false)
    const [loadingMemoryExtractionPrompt, setLoadingMemoryExtractionPrompt] = useState(false)
    const [savingMemoryExtractionPrompt, setSavingMemoryExtractionPrompt] = useState(false)
    const [editingMemoryExtractionPrompt, setEditingMemoryExtractionPrompt] = useState('')
    const [defaultMemoryExtractionPrompt, setDefaultMemoryExtractionPrompt] = useState('')
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [showProviderSettings, setShowProviderSettings] = useState(false)
    const [showMemoryProviderSettings, setShowMemoryProviderSettings] = useState(false)
    const [showPromptModal, setShowPromptModal] = useState(false)
    const [replyWaitConfig, setReplyWaitConfigState] = useState<ReplyWaitWindowConfig>(() => getReplyWaitWindowConfig())
    const [editingReplyWaitConfig, setEditingReplyWaitConfig] = useState<ReplyWaitWindowConfig>(() =>
        getReplyWaitWindowConfig()
    )
    const [showReplyWaitModal, setShowReplyWaitModal] = useState(false)
    const promptModalRef = useRef<HTMLDivElement>(null)
    const replyWaitModalRef = useRef<HTMLDivElement>(null)
    const memoryExtractionRoundsModalRef = useRef<HTMLDivElement>(null)
    const memoryExtractionPromptModalRef = useRef<HTMLDivElement>(null)

    useEffect(() => {
        loadData()
    }, [])

    useEffect(() => {
        if (showPromptModal && promptModalRef.current) {
            gsap.fromTo(
                promptModalRef.current,
                { opacity: 0, scale: 0.9 },
                { opacity: 1, scale: 1, duration: 0.2, ease: 'power2.out' }
            )
        }
    }, [showPromptModal])

    useEffect(() => {
        if (showReplyWaitModal && replyWaitModalRef.current) {
            gsap.fromTo(
                replyWaitModalRef.current,
                { opacity: 0, scale: 0.9 },
                { opacity: 1, scale: 1, duration: 0.2, ease: 'power2.out' }
            )
        }
    }, [showReplyWaitModal])

    useEffect(() => {
        if (showMemoryExtractionRoundsModal && memoryExtractionRoundsModalRef.current) {
            gsap.fromTo(
                memoryExtractionRoundsModalRef.current,
                { opacity: 0, scale: 0.9 },
                { opacity: 1, scale: 1, duration: 0.2, ease: 'power2.out' }
            )
        }
    }, [showMemoryExtractionRoundsModal])

    useEffect(() => {
        if (showMemoryExtractionPromptModal && memoryExtractionPromptModalRef.current) {
            gsap.fromTo(
                memoryExtractionPromptModalRef.current,
                { opacity: 0, scale: 0.9 },
                { opacity: 1, scale: 1, duration: 0.2, ease: 'power2.out' }
            )
        }
    }, [showMemoryExtractionPromptModal])

    const loadData = async ({ showLoading = true }: { showLoading?: boolean } = {}) => {
        if (showLoading) setLoading(true)
        const providersData = await getProviders()
        if (providersData) {
            setSystemPrompt(providersData.system_prompt)
            const activeProvider = providersData.providers.find((p) => p.id === providersData.active_provider_id)
            setActiveProviderName(activeProvider?.name || '未设置')
            setMemoryProvider(providersData.memory_provider || null)
            setMemoryEnabled(!!providersData.memory_enabled)
        }
        try {
            const settings = await memoryService.getMemoryExtractionSettings()
            setMemoryExtractionSettings(settings)
            setMemoryExtractionRounds(settings.rounds)
            setMemoryExtractionMaxRounds(settings.max_rounds)
            setMemoryExtractionProviderName(settings.provider_name || '')
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
        if (promptModalRef.current) {
            gsap.to(promptModalRef.current, {
                opacity: 0,
                scale: 0.9,
                duration: 0.2,
                ease: 'power2.in',
                onComplete: () => {
                    setShowPromptModal(false)
                },
            })
        } else {
            setShowPromptModal(false)
        }
    }

    const handleOpenReplyWaitModal = () => {
        setEditingReplyWaitConfig(replyWaitConfig)
        setShowReplyWaitModal(true)
    }

    const handleCloseReplyWaitModal = () => {
        if (replyWaitModalRef.current) {
            gsap.to(replyWaitModalRef.current, {
                opacity: 0,
                scale: 0.9,
                duration: 0.2,
                ease: 'power2.in',
                onComplete: () => {
                    setShowReplyWaitModal(false)
                },
            })
        } else {
            setShowReplyWaitModal(false)
        }
    }

    const handleOpenMemoryExtractionRoundsModal = () => {
        const rounds = memoryExtractionSettings?.rounds || memoryExtractionRounds || 5
        setEditingMemoryExtractionRounds(rounds)
        setShowMemoryExtractionRoundsModal(true)
    }

    const handleCloseMemoryExtractionRoundsModal = () => {
        if (memoryExtractionRoundsModalRef.current) {
            gsap.to(memoryExtractionRoundsModalRef.current, {
                opacity: 0,
                scale: 0.9,
                duration: 0.2,
                ease: 'power2.in',
                onComplete: () => {
                    setShowMemoryExtractionRoundsModal(false)
                },
            })
        } else {
            setShowMemoryExtractionRoundsModal(false)
        }
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
        if (memoryExtractionPromptModalRef.current) {
            gsap.to(memoryExtractionPromptModalRef.current, {
                opacity: 0,
                scale: 0.9,
                duration: 0.2,
                ease: 'power2.in',
                onComplete: () => {
                    setShowMemoryExtractionPromptModal(false)
                },
            })
        } else {
            setShowMemoryExtractionPromptModal(false)
        }
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
        loadData({ showLoading: false })
    }

    const handleMemoryProviderSettingsBack = () => {
        setShowMemoryProviderSettings(false)
        loadData({ showLoading: false })
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

    const memoryProviderPreview = getMemoryProviderPreview()

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
                    </div>

                    {/* 长期记忆设置 */}
                    <div className="settings-section">
                        <h3>长期记忆</h3>

                        <p className="prompt-modal-hint">
                            提示：开启后会将最近 {memoryExtractionRounds || 5} 轮对话片段发送给记忆处理模型用于提取，请勿输入敏感信息。
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

            {/* 供应商管理二级界面 */}
            {showProviderSettings && <ProviderSettings onBack={handleProviderSettingsBack} />}

            {/* 记忆提供商二级界面 */}
            {showMemoryProviderSettings && <MemoryProviderSettings onBack={handleMemoryProviderSettingsBack} />}

            {/* 系统提示词编辑弹窗 */}
            {showPromptModal && (
                <div className="prompt-modal-overlay" onClick={handleClosePromptModal}>
                    <div className="prompt-modal-card" ref={promptModalRef} onClick={(e) => e.stopPropagation()}>
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
                    </div>
                </div>
            )}

            {/* 回复等候窗口设置弹窗 */}
            {showReplyWaitModal && (
                <div className="prompt-modal-overlay" onClick={handleCloseReplyWaitModal}>
                    <div className="prompt-modal-card" ref={replyWaitModalRef} onClick={(e) => e.stopPropagation()}>
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
                    </div>
                </div>
            )}

            {/* 记忆提取轮数设置弹窗 */}
            {showMemoryExtractionRoundsModal && (
                <div className="prompt-modal-overlay" onClick={handleCloseMemoryExtractionRoundsModal}>
                    <div
                        className="prompt-modal-card"
                        ref={memoryExtractionRoundsModalRef}
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
                            <button className="prompt-modal-btn cancel" onClick={handleCloseMemoryExtractionRoundsModal}>
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
                    </div>
                </div>
            )}

            {/* 记忆提取提示词编辑弹窗 */}
            {showMemoryExtractionPromptModal && (
                <div className="prompt-modal-overlay" onClick={handleCloseMemoryExtractionPromptModal}>
                    <div
                        className="prompt-modal-card"
                        ref={memoryExtractionPromptModalRef}
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
                                必须保留 &#123;&#123;EXISTING_MEMORIES&#125;&#125; 与 &#123;&#123;CHAT_CONTENT&#125;&#125; 占位符。
                                可用变量：&#123;&#123;user&#125;&#125;（用户） / &#123;&#123;avatar&#125;&#125;（角色）
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
                            <button className="prompt-modal-btn cancel" onClick={handleCloseMemoryExtractionPromptModal}>
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
                    </div>
                </div>
            )}
        </div>
    )
}

export default Settings
