import { useEffect, useMemo, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import type { Provider, ProviderType } from '../types/chat'
import { getProviders, updateMemoryProvider } from '../services/api'
import {
    getProviderTypesChat,
    getOpenAIReasoningEffortOptions,
    getGeminiThinkingModes,
    getGeminiThinkingLevels,
    getGeminiThinkingBudgetRange,
    clampGeminiThinkingBudget,
    CustomSelect,
} from './provider'
import { NumericInput } from './NumericInput'
import { useT } from '../contexts/I18nContext'
import { centerModalVariants, drawerVariants, overlayVariants } from '../utils/motion'
import './ProviderSettings.css'

interface MemoryProviderSettingsProps {
    onBack: () => void
}

const MemoryProviderSettings: React.FC<MemoryProviderSettingsProps> = ({ onBack }) => {
    const { t } = useT()
    const [providers, setProviders] = useState<Provider[]>([])
    const [activeProviderId, setActiveProviderId] = useState('')
    const [memoryProvider, setMemoryProvider] = useState<Provider | null>(null)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [message, setMessage] = useState('')
    const [messageType, setMessageType] = useState<'success' | 'error'>('success')
    const [showModal, setShowModal] = useState(false)
    const [editingProvider, setEditingProvider] = useState<Provider | null>(null)
    const providerTypesChat = getProviderTypesChat()
    const openAIReasoningEffortOptions = getOpenAIReasoningEffortOptions()
    const geminiThinkingModes = getGeminiThinkingModes()
    const geminiThinkingLevels = getGeminiThinkingLevels()

    const emptyProvider: Provider = {
        id: 'memory',
        name: '',
        type: 'openai',
        base_url: '',
        api_key: '',
        model: '',
        temperature: 0.8,
        top_p: 1,
        thinking_budget: 0,
        reasoning_effort: '',
        gemini_thinking_mode: 'none',
        gemini_thinking_level: 'low',
        gemini_thinking_budget: 128,
        context_messages: 64,
        stream: true,
        image_capable: false,
    }

    useEffect(() => {
        loadData()
    }, [])

    const activeChatProvider = useMemo(() => {
        return providers.find((p) => p.id === activeProviderId) || null
    }, [providers, activeProviderId])

    const loadData = async () => {
        setLoading(true)
        const data = await getProviders()
        if (data) {
            setProviders(data.providers || [])
            setActiveProviderId(data.active_provider_id)
            setMemoryProvider(data.memory_provider || null)
        }
        setLoading(false)
    }

    const handleBack = () => {
        onBack()
    }

    const showMessageToast = (msg: string, type: 'success' | 'error' = 'success') => {
        setMessage(msg)
        setMessageType(type)
        setTimeout(() => {
            setMessage('')
            setMessageType('success')
        }, 2000)
    }

    const handleUseFollowChat = async () => {
        if (saving) return
        setSaving(true)
        const updated = await updateMemoryProvider(false)
        if (updated !== undefined) {
            setMemoryProvider(null)
            showMessageToast(t('settings.followChatModel'))
        } else {
            showMessageToast(t('settings.settingFailed'), 'error')
        }
        setSaving(false)
    }

    const openEditModal = () => {
        const base = memoryProvider
            ? {
                  ...memoryProvider,
                  api_key: '',
                  thinking_budget: memoryProvider.thinking_budget ?? 0,
                  reasoning_effort: memoryProvider.reasoning_effort ?? '',
                  gemini_thinking_mode: memoryProvider.gemini_thinking_mode || 'none',
                  gemini_thinking_level: memoryProvider.gemini_thinking_level || 'low',
                  gemini_thinking_budget: memoryProvider.gemini_thinking_budget || 128,
                  temperature: memoryProvider.type === 'anthropic' ? 1 : memoryProvider.temperature,
              }
            : { ...emptyProvider }

        setEditingProvider(base)
        setShowModal(true)
    }

    const handleCloseModal = () => {
        setShowModal(false)
        setEditingProvider(null)
    }

    const handleProviderChange = (field: keyof Provider, value: string | boolean | number) => {
        if (!editingProvider) return

        if (field === 'model') {
            const nextModel = String(value || '')
            const nextProvider: Provider = { ...editingProvider, model: nextModel }
            if (nextProvider.type === 'gemini' && nextProvider.gemini_thinking_mode === 'thinking_budget') {
                const nextBudget = Number(nextProvider.gemini_thinking_budget) || 0
                nextProvider.gemini_thinking_budget = clampGeminiThinkingBudget(nextModel, nextBudget)
            }
            setEditingProvider(nextProvider)
            return
        }

        if (field === 'type') {
            const nextType = value as ProviderType
            const nextProvider: Provider = {
                ...editingProvider,
                type: nextType,
                temperature: nextType === 'anthropic' ? 1 : editingProvider.temperature,
            }
            setEditingProvider(nextProvider)
            return
        }

        if (field === 'gemini_thinking_mode') {
            const nextMode = value as string
            const nextProvider: Provider = {
                ...editingProvider,
                gemini_thinking_mode: nextMode,
            }
            if (nextMode === 'thinking_level' && !nextProvider.gemini_thinking_level) {
                nextProvider.gemini_thinking_level = 'low'
            }
            if (nextMode === 'thinking_budget') {
                const nextBudget =
                    Number(nextProvider.gemini_thinking_budget) || getGeminiThinkingBudgetRange(nextProvider.model).min
                nextProvider.gemini_thinking_budget = clampGeminiThinkingBudget(nextProvider.model, nextBudget)
            }
            setEditingProvider(nextProvider)
            return
        }

        if (field === 'gemini_thinking_budget') {
            const nextBudget = Number(value) || 0
            setEditingProvider({
                ...editingProvider,
                gemini_thinking_budget: clampGeminiThinkingBudget(editingProvider.model, nextBudget),
            })
            return
        }

        setEditingProvider({ ...editingProvider, [field]: value })
    }

    const handleSaveProvider = async () => {
        if (!editingProvider) return

        if (!editingProvider.id || !editingProvider.name) {
            showMessageToast(t('memoryProvider.idRequired'), 'error')
            return
        }

        if (!editingProvider.base_url.trim()) {
            showMessageToast(t('memoryProvider.apiUrlRequired'), 'error')
            return
        }

        if (!editingProvider.model.trim()) {
            showMessageToast(t('memoryProvider.modelRequired'), 'error')
            return
        }

        const hasStoredApiKey = Boolean(memoryProvider?.api_key)
        if (!hasStoredApiKey && !editingProvider.api_key.trim()) {
            showMessageToast(t('memoryProvider.apiKeyRequired'), 'error')
            return
        }

        if (editingProvider.type === 'gemini_image') {
            showMessageToast(t('memoryProvider.geminiImageNotAllowed'), 'error')
            return
        }

        setSaving(true)
        const updated = await updateMemoryProvider(true, editingProvider)
        if (updated) {
            setMemoryProvider(updated)
            showMessageToast(t('memoryProvider.saved'))
            handleCloseModal()
        } else {
            showMessageToast(t('common.saveFailed'), 'error')
        }
        setSaving(false)
    }

    const isFollowChat = memoryProvider == null

    return (
        <motion.div
            className="provider-settings"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={drawerVariants}
        >
            <div className="provider-settings-header">
                <button className="back-button" onClick={handleBack}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('memoryProvider.title')}</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : (
                <div className="provider-settings-content">
                    <div style={{ marginBottom: 12, color: 'var(--text-secondary)', fontSize: 12, lineHeight: 1.4 }}>
                        {t('memoryProvider.hint')}
                    </div>

                    <div className="provider-cards">
                        <div
                            className={`provider-card ${isFollowChat ? 'active' : 'inactive'}`}
                            onClick={() => {
                                if (isFollowChat) return
                                handleUseFollowChat()
                            }}
                        >
                            <div className="provider-card-header">
                                <div className="provider-card-id">{t('settings.followChatModel')}</div>
                                {isFollowChat && <div className="active-indicator">{t('common.current')}</div>}
                            </div>
                            <div className="provider-card-body">
                                <div className="provider-card-row">
                                    <span className="provider-card-label">{t('imageProvider.name')}</span>
                                    <span className="provider-card-value">
                                        {activeChatProvider?.name || t('common.notSet')}
                                    </span>
                                </div>
                                <div className="provider-card-row">
                                    <span className="provider-card-label">{t('provider.model')}</span>
                                    <span className="provider-card-value model">
                                        {activeChatProvider?.model || t('common.notSet')}
                                    </span>
                                </div>
                                <div className="provider-card-row">
                                    <span className="provider-card-label">{t('imageProvider.type')}</span>
                                    <span className="provider-card-value type">
                                        {activeChatProvider?.type || t('memoryProvider.unknown')}
                                    </span>
                                </div>
                            </div>
                        </div>

                        <div
                            className={`provider-card ${!isFollowChat ? 'active' : 'inactive'}`}
                            onClick={() => openEditModal()}
                        >
                            <div className="provider-card-header">
                                <div className="provider-card-id">{t('memoryProvider.independent')}</div>
                                {!isFollowChat && <div className="active-indicator">{t('common.current')}</div>}
                            </div>
                            <div className="provider-card-body">
                                <div className="provider-card-row">
                                    <span className="provider-card-label">{t('imageProvider.name')}</span>
                                    <span className="provider-card-value">
                                        {memoryProvider?.name || t('common.notConfigured')}
                                    </span>
                                </div>
                                <div className="provider-card-row">
                                    <span className="provider-card-label">{t('provider.model')}</span>
                                    <span className="provider-card-value model">
                                        {memoryProvider?.model || t('common.notConfigured')}
                                    </span>
                                </div>
                                <div className="provider-card-row">
                                    <span className="provider-card-label">{t('imageProvider.type')}</span>
                                    <span className="provider-card-value type">
                                        {memoryProvider?.type || t('memoryProvider.unknown')}
                                    </span>
                                </div>
                            </div>
                            <div className="provider-card-actions">
                                <button
                                    className="card-action-btn edit"
                                    onClick={(e) => {
                                        e.stopPropagation()
                                        openEditModal()
                                    }}
                                >
                                    <svg viewBox="0 0 24 24">
                                        <path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04a1.003 1.003 0 000-1.42l-2.34-2.34a1.003 1.003 0 00-1.42 0l-1.83 1.83 3.75 3.75 1.84-1.82z" />
                                    </svg>
                                    {memoryProvider ? t('common.edit') : t('common.configure')}
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            )}

            <AnimatePresence>
                {showModal && editingProvider && (
                    <motion.div
                        className="modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseModal}
                    >
                        <motion.div
                            className="modal-card"
                            initial="hidden"
                            animate="visible"
                            exit="hidden"
                            variants={centerModalVariants}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="modal-header">
                                <h3>{t('memoryProvider.configTitle')}</h3>
                                <button className="modal-close" onClick={handleCloseModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="modal-body">
                                <div className="modal-group">
                                    <label className="modal-label">{t('memoryProvider.providerId')}</label>
                                    <input
                                        type="text"
                                        className="modal-input"
                                        value={editingProvider.id}
                                        onChange={(e) => handleProviderChange('id', e.target.value)}
                                        placeholder="memory"
                                    />
                                </div>

                                <div className="modal-group">
                                    <label className="modal-label">{t('provider.displayName')}</label>
                                    <input
                                        type="text"
                                        className="modal-input"
                                        value={editingProvider.name}
                                        onChange={(e) => handleProviderChange('name', e.target.value)}
                                        placeholder={t('memoryProvider.title')}
                                    />
                                </div>

                                <div className="modal-group">
                                    <label className="modal-label">{t('provider.type')}</label>
                                    <CustomSelect
                                        value={editingProvider.type || 'openai'}
                                        options={providerTypesChat}
                                        ariaLabel={t('provider.type')}
                                        onChange={(value) => handleProviderChange('type', value)}
                                    />
                                </div>

                                <div className="modal-group">
                                    <label className="modal-label">{t('memoryProvider.apiUrl')}</label>
                                    <input
                                        type="text"
                                        className="modal-input"
                                        value={editingProvider.base_url}
                                        onChange={(e) => handleProviderChange('base_url', e.target.value)}
                                        placeholder="https://api.openai.com/v1"
                                    />
                                </div>

                                <div className="modal-group">
                                    <label className="modal-label">{t('memoryProvider.apiKey')}</label>
                                    <input
                                        type="password"
                                        className="modal-input"
                                        value={editingProvider.api_key}
                                        onChange={(e) => handleProviderChange('api_key', e.target.value)}
                                        placeholder={memoryProvider ? t('memoryProvider.apiKeyHint') : 'sk-...'}
                                    />
                                </div>

                                <div className="modal-group">
                                    <label className="modal-label">{t('memoryProvider.model')}</label>
                                    <input
                                        type="text"
                                        className="modal-input"
                                        value={editingProvider.model}
                                        onChange={(e) => handleProviderChange('model', e.target.value)}
                                        placeholder="gpt-4"
                                    />
                                </div>

                                <div className="modal-group">
                                    <label className="modal-label">{t('memoryProvider.temperature')}</label>
                                    <NumericInput
                                        className="modal-input"
                                        min={0}
                                        max={2}
                                        step={0.1}
                                        value={editingProvider.temperature}
                                        parseAs="float"
                                        onValueChange={(value) => handleProviderChange('temperature', value)}
                                        placeholder="0.8"
                                        disabled={editingProvider.type === 'anthropic'}
                                    />
                                </div>

                                <div className="modal-group">
                                    <label className="modal-label">{t('memoryProvider.topP')}</label>
                                    <NumericInput
                                        className="modal-input"
                                        min={0}
                                        max={1}
                                        step={0.1}
                                        value={editingProvider.top_p}
                                        parseAs="float"
                                        onValueChange={(value) => handleProviderChange('top_p', value)}
                                        placeholder="1"
                                    />
                                </div>

                                {(editingProvider.type === 'openai' || editingProvider.type === 'openai_response') && (
                                    <div className="modal-group">
                                        <label className="modal-label">{t('memoryProvider.reasoningEffort')}</label>
                                        <CustomSelect
                                            value={editingProvider.reasoning_effort ?? ''}
                                            options={openAIReasoningEffortOptions}
                                            ariaLabel={t('memoryProvider.reasoningEffort')}
                                            onChange={(value) => handleProviderChange('reasoning_effort', value)}
                                        />
                                    </div>
                                )}

                                {editingProvider.type === 'gemini' && (
                                    <>
                                        <div className="modal-group">
                                            <label className="modal-label">
                                                {t('memoryProvider.geminiThinkingMode')}
                                            </label>
                                            <CustomSelect
                                                value={editingProvider.gemini_thinking_mode || 'none'}
                                                options={geminiThinkingModes}
                                                ariaLabel={t('memoryProvider.geminiThinkingMode')}
                                                onChange={(value) =>
                                                    handleProviderChange('gemini_thinking_mode', value)
                                                }
                                            />
                                        </div>

                                        <div className="modal-group">
                                            <label className="modal-label">
                                                {t('memoryProvider.geminiThinkingParams')}
                                            </label>
                                            {editingProvider.gemini_thinking_mode === 'thinking_level' && (
                                                <CustomSelect
                                                    value={editingProvider.gemini_thinking_level || 'low'}
                                                    options={geminiThinkingLevels}
                                                    ariaLabel={t('memoryProvider.geminiThinkingParams')}
                                                    onChange={(value) =>
                                                        handleProviderChange('gemini_thinking_level', value)
                                                    }
                                                />
                                            )}
                                            {editingProvider.gemini_thinking_mode === 'thinking_budget' && (
                                                <NumericInput
                                                    className="modal-input"
                                                    min={getGeminiThinkingBudgetRange(editingProvider.model).min}
                                                    max={getGeminiThinkingBudgetRange(editingProvider.model).max}
                                                    step={1}
                                                    value={editingProvider.gemini_thinking_budget}
                                                    parseAs="int"
                                                    onValueChange={(value) =>
                                                        handleProviderChange('gemini_thinking_budget', value)
                                                    }
                                                    placeholder={`${getGeminiThinkingBudgetRange(editingProvider.model).min}-${getGeminiThinkingBudgetRange(editingProvider.model).max}`}
                                                />
                                            )}
                                            {editingProvider.gemini_thinking_mode === 'none' && (
                                                <input
                                                    type="text"
                                                    className="modal-input"
                                                    value={t('common.disabled')}
                                                    disabled
                                                />
                                            )}
                                        </div>
                                    </>
                                )}

                                {editingProvider.type === 'anthropic' && (
                                    <div className="modal-group">
                                        <label className="modal-label">{t('memoryProvider.thinkingBudget')}</label>
                                        <NumericInput
                                            className="modal-input"
                                            min={0}
                                            step={1}
                                            value={editingProvider.thinking_budget}
                                            parseAs="int"
                                            onValueChange={(value) => handleProviderChange('thinking_budget', value)}
                                            placeholder="0"
                                        />
                                    </div>
                                )}

                                {/* 记忆提取不需要上下文轮数、流式输出和识图能力配置 */}
                            </div>

                            <div className="modal-footer">
                                <button className="modal-btn cancel" onClick={handleCloseModal}>
                                    {t('common.cancel')}
                                </button>
                                <button className="modal-btn save" onClick={handleSaveProvider} disabled={saving}>
                                    {saving ? t('common.saving') : t('common.save')}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            {message && <div className={`provider-message ${messageType}`}>{message}</div>}
        </motion.div>
    )
}

export default MemoryProviderSettings
