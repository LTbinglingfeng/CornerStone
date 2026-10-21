import { useEffect, useMemo, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import type { Provider, ProviderType } from '../types/chat'
import { getProviders, updateMemoryProvider } from '../services/api'
import {
    getProviderTypesChat,
    getAnthropicPromptCacheTTLOptions,
    getOpenAIReasoningEffortOptions,
    getGeminiThinkingModes,
    getGeminiThinkingLevels,
    getGeminiThinkingBudgetRange,
    clampGeminiThinkingBudget,
    CustomSelect,
    SamplingParamsFields,
} from './provider'
import { NumericInput } from './NumericInput'
import { ModalOverlay } from './ModalOverlay'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { Group, Icon, Row, Toggle } from './ui'
import { centerModalVariants, drawerVariants, overlayVariants } from '../utils/motion'
import './ProviderSettings.css'

interface MemoryProviderSettingsProps {
    onBack: () => void
}

const MemoryProviderSettings: React.FC<MemoryProviderSettingsProps> = ({ onBack }) => {
    const { t } = useT()
    const { showToast } = useToast()
    const [providers, setProviders] = useState<Provider[]>([])
    const [activeProviderId, setActiveProviderId] = useState('')
    const [memoryProvider, setMemoryProvider] = useState<Provider | null>(null)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [showModal, setShowModal] = useState(false)
    const [editingProvider, setEditingProvider] = useState<Provider | null>(null)
    const providerTypesChat = getProviderTypesChat()
    const anthropicPromptCacheTTLOptions = getAnthropicPromptCacheTTLOptions()
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
        temperature_enabled: false,
        top_p: 1,
        top_p_enabled: false,
        thinking_budget: 0,
        prompt_caching: false,
        prompt_cache_ttl: '5m',
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

    const showMessageToast = (msg: string, type: 'success' | 'error' = 'success') => showToast(msg, type)

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
                  prompt_caching: memoryProvider.prompt_caching ?? false,
                  prompt_cache_ttl: memoryProvider.prompt_cache_ttl || '5m',
                  reasoning_effort: memoryProvider.reasoning_effort ?? '',
                  gemini_thinking_mode: memoryProvider.gemini_thinking_mode || 'none',
                  gemini_thinking_level: memoryProvider.gemini_thinking_level || 'low',
                  gemini_thinking_budget: memoryProvider.gemini_thinking_budget || 128,
                  temperature: memoryProvider.temperature ?? 0.8,
                  temperature_enabled: memoryProvider.temperature_enabled ?? false,
                  top_p: memoryProvider.top_p || 1,
                  top_p_enabled: memoryProvider.top_p_enabled ?? false,
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
                prompt_cache_ttl: editingProvider.prompt_cache_ttl || '5m',
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
    const check = (selected: boolean) =>
        selected ? (
            <Icon name="check" className="provider-choice-check" />
        ) : (
            <span className="provider-choice-placeholder" aria-hidden="true" />
        )

    return (
        <motion.div
            className="provider-settings"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={drawerVariants}
        >
            <div className="provider-settings-header">
                <button className="back-button" onClick={handleBack} aria-label={t('common.back')}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('memoryProvider.title')}</div>
            </div>

            {loading ? (
                <div className="provider-settings-loading" role="status">
                    <span className="ui-spinner" aria-hidden="true" />
                    {t('common.loading')}
                </div>
            ) : (
                <div className="provider-settings-content">
                    <Group title={t('settings.memoryProvider')} description={t('memoryProvider.hint')}>
                        <Row
                            icon="message"
                            label={t('settings.followChatModel')}
                            detail={
                                activeChatProvider
                                    ? `${activeChatProvider.name || activeChatProvider.id}${activeChatProvider.model ? ` · ${activeChatProvider.model}` : ''}`
                                    : t('settingsManagement.memoryProvider.followDetail')
                            }
                            aria-pressed={isFollowChat}
                            disabled={saving}
                            chevron={false}
                            trailing={check(isFollowChat)}
                            onClick={() => {
                                if (isFollowChat) return
                                void handleUseFollowChat()
                            }}
                        />
                        <Row
                            icon="bot"
                            label={t('memoryProvider.independent')}
                            detail={
                                memoryProvider
                                    ? `${memoryProvider.name || memoryProvider.id}${memoryProvider.model ? ` · ${memoryProvider.model}` : ''}`
                                    : t('settingsManagement.memoryProvider.independentDetail')
                            }
                            aria-pressed={!isFollowChat}
                            value={memoryProvider ? t('common.edit') : t('common.configure')}
                            trailing={check(!isFollowChat)}
                            onClick={openEditModal}
                        />
                    </Group>
                </div>
            )}

            <AnimatePresence>
                {showModal && editingProvider && (
                    <ModalOverlay
                        aria-label={t('memoryProvider.configTitle')}
                        onDismiss={() => {
                            if (!saving) handleCloseModal()
                        }}
                        className="modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                    >
                        <motion.div
                            className="modal-card provider-modal-card"
                            initial="hidden"
                            animate="visible"
                            exit="hidden"
                            variants={centerModalVariants}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <div className="modal-header">
                                <h3>{t('memoryProvider.configTitle')}</h3>
                                <button
                                    className="modal-close"
                                    onClick={handleCloseModal}
                                    disabled={saving}
                                    aria-label={t('common.close')}
                                >
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="modal-body">
                                <section className="provider-form-section">
                                    <h4 className="provider-form-section__title">
                                        {t('settingsManagement.provider.basics')}
                                    </h4>
                                    <div className="provider-form-grid">
                                        <div className="modal-group">
                                            <label className="modal-label">{t('memoryProvider.providerId')}</label>
                                            <input
                                                type="text"
                                                className="modal-input"
                                                aria-label={t('memoryProvider.providerId')}
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
                                                aria-label={t('provider.displayName')}
                                                value={editingProvider.name}
                                                onChange={(e) => handleProviderChange('name', e.target.value)}
                                                placeholder={t('memoryProvider.title')}
                                            />
                                        </div>
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
                                </section>

                                <section className="provider-form-section">
                                    <h4 className="provider-form-section__title">
                                        {t('settingsManagement.provider.connection')}
                                    </h4>

                                    <div className="modal-group">
                                        <label className="modal-label">{t('memoryProvider.apiUrl')}</label>
                                        <input
                                            type="text"
                                            className="modal-input"
                                            aria-label={t('memoryProvider.apiUrl')}
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
                                            aria-label={t('memoryProvider.apiKey')}
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
                                            aria-label={t('memoryProvider.model')}
                                            value={editingProvider.model}
                                            onChange={(e) => handleProviderChange('model', e.target.value)}
                                            placeholder="gpt-4"
                                        />
                                    </div>
                                </section>

                                <section className="provider-form-section">
                                    <h4 className="provider-form-section__title">
                                        {t('settingsManagement.provider.parameters')}
                                    </h4>
                                    <SamplingParamsFields provider={editingProvider} onChange={handleProviderChange} />

                                    {(editingProvider.type === 'openai' ||
                                        editingProvider.type === 'openai_response') && (
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
                                                        aria-label={t('memoryProvider.geminiThinkingParams')}
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
                                                        aria-label={t('memoryProvider.geminiThinkingParams')}
                                                        value={t('common.disabled')}
                                                        disabled
                                                    />
                                                )}
                                            </div>
                                        </>
                                    )}

                                    {editingProvider.type === 'anthropic' && (
                                        <>
                                            <div className="modal-group">
                                                <label className="modal-label">
                                                    {t('memoryProvider.thinkingBudget')}
                                                </label>
                                                <NumericInput
                                                    className="modal-input"
                                                    min={0}
                                                    step={1}
                                                    aria-label={t('memoryProvider.thinkingBudget')}
                                                    value={editingProvider.thinking_budget}
                                                    parseAs="int"
                                                    onValueChange={(value) =>
                                                        handleProviderChange('thinking_budget', value)
                                                    }
                                                    placeholder="0"
                                                />
                                            </div>

                                            <div className="provider-form-switch">
                                                <span className="modal-label">{t('provider.promptCaching')}</span>
                                                <Toggle
                                                    label={t('provider.promptCaching')}
                                                    checked={Boolean(editingProvider.prompt_caching)}
                                                    onChange={(checked) =>
                                                        handleProviderChange('prompt_caching', checked)
                                                    }
                                                />
                                            </div>

                                            {editingProvider.prompt_caching && (
                                                <div className="modal-group">
                                                    <label className="modal-label">
                                                        {t('provider.promptCacheTTL')}
                                                    </label>
                                                    <CustomSelect
                                                        value={editingProvider.prompt_cache_ttl || '5m'}
                                                        options={anthropicPromptCacheTTLOptions}
                                                        ariaLabel={t('provider.promptCacheTTL')}
                                                        onChange={(value) =>
                                                            handleProviderChange('prompt_cache_ttl', value)
                                                        }
                                                    />
                                                </div>
                                            )}
                                        </>
                                    )}

                                    {/* 记忆提取不需要上下文轮数、流式输出和识图能力配置 */}
                                </section>
                            </div>

                            <div className="modal-footer">
                                <button className="modal-btn cancel" onClick={handleCloseModal} disabled={saving}>
                                    {t('common.cancel')}
                                </button>
                                <button className="modal-btn save" onClick={handleSaveProvider} disabled={saving}>
                                    {saving ? t('common.saving') : t('common.save')}
                                </button>
                            </div>
                        </motion.div>
                    </ModalOverlay>
                )}
            </AnimatePresence>
        </motion.div>
    )
}

export default MemoryProviderSettings
