import { useState, useEffect } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import type { Provider, ProviderType } from '../types/chat'
import { getProviders, addProvider, updateProvider, deleteProvider, setActiveProvider } from '../services/api'
import {
    getProviderTypesAll,
    getAnthropicPromptCacheTTLOptions,
    getOpenAIReasoningEffortOptions,
    getGeminiThinkingModes,
    getGeminiThinkingLevels,
    GEMINI_IMAGE_ASPECT_RATIOS,
    getGeminiImageSizes,
    GEMINI_IMAGE_OUTPUT_MIME_TYPES,
    getGeminiThinkingBudgetRange,
    clampGeminiThinkingBudget,
    CustomSelect,
    ModelSelect,
    SamplingParamsFields,
} from './provider'
import { ModalOverlay } from './ModalOverlay'
import { NumericInput } from './NumericInput'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import { Badge, EmptyState, Icon, Toggle } from './ui'
import { centerModalVariants, drawerVariants, overlayVariants } from '../utils/motion'
import './ProviderSettings.css'

interface ProviderSettingsProps {
    onBack: () => void
}

const ProviderSettings: React.FC<ProviderSettingsProps> = ({ onBack }) => {
    const { t } = useT()
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [providers, setProviders] = useState<Provider[]>([])
    const [activeProviderId, setActiveProviderId] = useState('')
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [showModal, setShowModal] = useState(false)
    const [editingProvider, setEditingProvider] = useState<Provider | null>(null)
    const [isAddingNew, setIsAddingNew] = useState(false)
    const providerTypesAll = getProviderTypesAll()
    const anthropicPromptCacheTTLOptions = getAnthropicPromptCacheTTLOptions()
    const openAIReasoningEffortOptions = getOpenAIReasoningEffortOptions()
    const geminiThinkingModes = getGeminiThinkingModes()
    const geminiThinkingLevels = getGeminiThinkingLevels()
    const geminiImageSizes = getGeminiImageSizes()

    const emptyProvider: Provider = {
        id: '',
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
        gemini_automatic_continuation: false,
        gemini_image_aspect_ratio: '1:1',
        gemini_image_size: '',
        gemini_image_number_of_images: 1,
        gemini_image_output_mime_type: 'image/jpeg',
        context_messages: 64,
        stream: true,
        image_capable: false,
    }

    useEffect(() => {
        loadProviders()
    }, [])

    const loadProviders = async () => {
        setLoading(true)
        const data = await getProviders()
        if (data) {
            setProviders(data.providers)
            setActiveProviderId(data.active_provider_id)
        }
        setLoading(false)
    }

    const handleBack = () => {
        onBack()
    }

    const handleSetActive = async (providerId: string) => {
        const success = await setActiveProvider(providerId)
        if (success) {
            setActiveProviderId(providerId)
            showToast(t('settings.currentProvider'), 'success')
        } else {
            showToast(t('settings.settingFailed'), 'error')
        }
    }

    const handleAddNew = () => {
        setEditingProvider({ ...emptyProvider })
        setIsAddingNew(true)
        setShowModal(true)
    }

    const handleEditProvider = (provider: Provider) => {
        setEditingProvider({
            ...provider,
            api_key: '',
            thinking_budget: provider.thinking_budget ?? 0,
            prompt_caching: provider.prompt_caching ?? false,
            prompt_cache_ttl: provider.prompt_cache_ttl || '5m',
            reasoning_effort: provider.reasoning_effort ?? '',
            gemini_thinking_mode: provider.gemini_thinking_mode || 'none',
            gemini_thinking_level: provider.gemini_thinking_level || 'low',
            gemini_thinking_budget: provider.gemini_thinking_budget || 128,
            gemini_automatic_continuation: provider.gemini_automatic_continuation ?? false,
            gemini_image_aspect_ratio: provider.gemini_image_aspect_ratio || '1:1',
            gemini_image_size: provider.gemini_image_size || '',
            gemini_image_number_of_images: provider.gemini_image_number_of_images ?? 1,
            gemini_image_output_mime_type: provider.gemini_image_output_mime_type || 'image/jpeg',
            temperature: provider.temperature ?? 0.8,
            temperature_enabled: provider.temperature_enabled ?? false,
            top_p: provider.top_p || 1,
            top_p_enabled: provider.top_p_enabled ?? false,
        })
        setIsAddingNew(false)
        setShowModal(true)
    }

    const handleCloseModal = () => {
        setShowModal(false)
        setEditingProvider(null)
        setIsAddingNew(false)
    }

    const handleSaveProvider = async () => {
        if (!editingProvider) return

        if (!editingProvider.id || !editingProvider.name) {
            showToast(t('memoryProvider.idRequired'), 'error')
            return
        }

        if (!editingProvider.base_url.trim()) {
            showToast(t('memoryProvider.apiUrlRequired'), 'error')
            return
        }

        if (!editingProvider.model.trim()) {
            showToast(t('memoryProvider.modelRequired'), 'error')
            return
        }

        if (editingProvider.type === 'gemini_image' && editingProvider.gemini_image_output_mime_type !== 'image/jpeg') {
            showToast(t('provider.imageOutputFormatUnsupported'), 'error')
            return
        }

        const hasStoredApiKey = !isAddingNew && Boolean(providers.find((p) => p.id === editingProvider.id)?.api_key)
        if ((isAddingNew || !hasStoredApiKey) && !editingProvider.api_key.trim()) {
            showToast(t('memoryProvider.apiKeyRequired'), 'error')
            return
        }

        setSaving(true)

        if (isAddingNew) {
            const result = await addProvider(editingProvider)
            if (result) {
                await loadProviders()
                showToast(t('provider.addSuccess'), 'success')
                handleCloseModal()
            } else {
                showToast(t('provider.addFailed'), 'error')
            }
        } else {
            const success = await updateProvider(editingProvider)
            if (success) {
                await loadProviders()
                showToast(t('common.save'), 'success')
                handleCloseModal()
            } else {
                showToast(t('provider.updateFailed'), 'error')
            }
        }

        setSaving(false)
    }

    const handleDeleteProvider = async (id: string) => {
        if (providers.length <= 1) {
            showToast(t('provider.keepAtLeastOne'), 'error')
            return
        }

        const ok = await confirm({
            title: t('provider.deleteProvider'),
            message: t('provider.deleteProviderConfirm'),
            confirmText: t('common.delete'),
            danger: true,
        })
        if (!ok) return

        const success = await deleteProvider(id)
        if (success) {
            await loadProviders()
            showToast(t('provider.providerDeleted'), 'success')
        } else {
            showToast(t('memory.deleteFailed'), 'error')
        }
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
            if (nextType !== 'gemini') {
                nextProvider.gemini_automatic_continuation = false
            }
            if (nextType === 'gemini_image') {
                nextProvider.gemini_image_aspect_ratio = nextProvider.gemini_image_aspect_ratio || '1:1'
                nextProvider.gemini_image_size = nextProvider.gemini_image_size || ''
                nextProvider.gemini_image_number_of_images = nextProvider.gemini_image_number_of_images ?? 1
                nextProvider.gemini_image_output_mime_type = nextProvider.gemini_image_output_mime_type || 'image/jpeg'
            } else {
                nextProvider.gemini_image_aspect_ratio = undefined
                nextProvider.gemini_image_size = undefined
                nextProvider.gemini_image_number_of_images = undefined
                nextProvider.gemini_image_output_mime_type = undefined
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

    const renderSwitch = (
        field: 'stream' | 'image_capable' | 'prompt_caching' | 'gemini_automatic_continuation',
        label: string,
        hint?: string
    ) => (
        <div className="provider-form-switch">
            <span className="provider-form-switch__text">
                <span className="modal-label">{label}</span>
                {hint && <span className="provider-form-switch__hint">{hint}</span>}
            </span>
            <Toggle
                label={label}
                checked={Boolean(editingProvider?.[field])}
                onChange={(checked) => handleProviderChange(field, checked)}
            />
        </div>
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
                <div className="provider-settings-title">{t('provider.title')}</div>
                <button
                    type="button"
                    className="ui-btn ui-btn--primary ui-btn--sm"
                    onClick={handleAddNew}
                    aria-label={t('provider.addProvider')}
                >
                    <Icon name="plus" />
                    {t('settingsManagement.provider.add')}
                </button>
            </div>

            {loading ? (
                <div className="provider-settings-loading" role="status">
                    <span className="ui-spinner" aria-hidden="true" />
                    {t('common.loading')}
                </div>
            ) : (
                <div className="provider-settings-content">
                    {providers.length === 0 ? (
                        <EmptyState
                            icon="bot"
                            title={t('settingsManagement.provider.empty')}
                            description={t('settingsManagement.provider.emptyHint')}
                            action={
                                <button type="button" className="ui-btn ui-btn--primary" onClick={handleAddNew}>
                                    <Icon name="plus" />
                                    {t('provider.addProvider')}
                                </button>
                            }
                        />
                    ) : (
                        <div className="ui-list provider-list">
                            {providers.map((provider) => {
                                const isActive = provider.id === activeProviderId
                                const isChatSelectable = provider.type !== 'gemini_image'
                                const displayName = provider.name || provider.id
                                const typeLabel =
                                    providerTypesAll.find((type) => type.value === provider.type)?.label ||
                                    provider.type ||
                                    t('provider.openaiCompatible')
                                return (
                                    <div key={provider.id} className={`provider-card${isActive ? ' active' : ''}`}>
                                        <div className="provider-card-main">
                                            <div className="provider-card-title">
                                                <span className="provider-card-name">{displayName}</span>
                                                <Badge>{typeLabel}</Badge>
                                                {isActive && (
                                                    <Badge tone="success" dot>
                                                        {t('imageProvider.inUse')}
                                                    </Badge>
                                                )}
                                            </div>
                                            <div className="provider-card-meta">
                                                <span className="settings-mono">
                                                    {provider.model || t('common.notSet')}
                                                </span>
                                                {provider.base_url && (
                                                    <span className="provider-card-url">{provider.base_url}</span>
                                                )}
                                            </div>
                                            {!isChatSelectable && (
                                                <p className="provider-card-note">{t('provider.imageOnly')}</p>
                                            )}
                                        </div>
                                        <div className="provider-card-actions">
                                            {!isActive && isChatSelectable && (
                                                <button
                                                    type="button"
                                                    className="ui-btn ui-btn--sm card-action-btn use"
                                                    onClick={() => handleSetActive(provider.id)}
                                                >
                                                    {t('console.useModel')}
                                                </button>
                                            )}
                                            <button
                                                type="button"
                                                className="ui-btn ui-btn--sm card-action-btn edit"
                                                onClick={() => handleEditProvider(provider)}
                                            >
                                                {t('common.edit')}
                                            </button>
                                            <button
                                                type="button"
                                                className="ui-icon-btn card-action-btn delete"
                                                aria-label={`${t('common.delete')} ${displayName}`}
                                                title={t('common.delete')}
                                                onClick={() => handleDeleteProvider(provider.id)}
                                            >
                                                <Icon name="trash" />
                                            </button>
                                        </div>
                                    </div>
                                )
                            })}
                        </div>
                    )}
                </div>
            )}

            <AnimatePresence>
                {showModal && editingProvider && (
                    <ModalOverlay
                        aria-label={isAddingNew ? t('provider.addProvider') : t('common.edit')}
                        onDismiss={handleCloseModal}
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
                                <h3>{isAddingNew ? t('provider.addProvider') : t('common.edit')}</h3>
                                <button
                                    className="modal-close"
                                    onClick={handleCloseModal}
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
                                            <label className="modal-label">{t('provider.id')}</label>
                                            <input
                                                aria-label={t('provider.id')}
                                                type="text"
                                                className="modal-input"
                                                value={editingProvider.id}
                                                onChange={(e) => handleProviderChange('id', e.target.value)}
                                                placeholder="unique-id"
                                                disabled={!isAddingNew}
                                            />
                                        </div>

                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.displayName')}</label>
                                            <input
                                                aria-label={t('provider.displayName')}
                                                type="text"
                                                className="modal-input"
                                                value={editingProvider.name}
                                                onChange={(e) => handleProviderChange('name', e.target.value)}
                                                placeholder="OpenAI"
                                            />
                                        </div>
                                    </div>

                                    <div className="modal-group">
                                        <label className="modal-label">{t('provider.type')}</label>
                                        <CustomSelect
                                            value={editingProvider.type || 'openai'}
                                            options={providerTypesAll}
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
                                        <label className="modal-label">{t('provider.apiUrl')}</label>
                                        <input
                                            aria-label={t('provider.apiUrl')}
                                            type="text"
                                            className="modal-input"
                                            value={editingProvider.base_url}
                                            onChange={(e) => handleProviderChange('base_url', e.target.value)}
                                            placeholder="https://api.openai.com/v1"
                                        />
                                    </div>

                                    <div className="modal-group">
                                        <label className="modal-label">{t('provider.apiKey')}</label>
                                        <input
                                            aria-label={t('provider.apiKey')}
                                            type="password"
                                            className="modal-input"
                                            value={editingProvider.api_key}
                                            onChange={(e) => handleProviderChange('api_key', e.target.value)}
                                            placeholder={isAddingNew ? 'sk-...' : t('memoryProvider.apiKeyHint')}
                                        />
                                    </div>

                                    <div className="modal-group">
                                        <label>
                                            <span className="modal-label">{t('provider.model')}</span>
                                            <ModelSelect
                                                value={editingProvider.model}
                                                providerId={editingProvider.id}
                                                providerType={editingProvider.type}
                                                baseUrl={editingProvider.base_url}
                                                apiKey={editingProvider.api_key}
                                                isNewProvider={isAddingNew}
                                                placeholder={
                                                    editingProvider.type === 'gemini_image'
                                                        ? 'gemini-nano-banana-2.1'
                                                        : 'gpt-4'
                                                }
                                                onChange={(value) => handleProviderChange('model', value)}
                                                onError={(message) => showToast(message, 'error')}
                                            />
                                        </label>
                                    </div>
                                </section>

                                <section className="provider-form-section">
                                    <h4 className="provider-form-section__title">
                                        {t('settingsManagement.provider.parameters')}
                                    </h4>
                                    {editingProvider.type === 'gemini_image' && (
                                        <p className="provider-form-switch__hint">
                                            {t('provider.imageInteractionsHint')}
                                        </p>
                                    )}
                                    {editingProvider.type === 'gemini_image' && (
                                        <div className="provider-form-grid">
                                            <div className="modal-group">
                                                <label className="modal-label">{t('provider.imageAspectRatio')}</label>
                                                <select
                                                    aria-label={t('provider.imageAspectRatio')}
                                                    className="modal-input modal-select"
                                                    value={editingProvider.gemini_image_aspect_ratio || '1:1'}
                                                    onChange={(e) =>
                                                        handleProviderChange(
                                                            'gemini_image_aspect_ratio',
                                                            e.target.value
                                                        )
                                                    }
                                                >
                                                    {GEMINI_IMAGE_ASPECT_RATIOS.map((ratio) => (
                                                        <option key={ratio.value} value={ratio.value}>
                                                            {ratio.label}
                                                        </option>
                                                    ))}
                                                </select>
                                            </div>

                                            <div className="modal-group">
                                                <label className="modal-label">{t('provider.imageResolution')}</label>
                                                <select
                                                    aria-label={t('provider.imageResolution')}
                                                    className="modal-input modal-select"
                                                    value={editingProvider.gemini_image_size || ''}
                                                    onChange={(e) =>
                                                        handleProviderChange('gemini_image_size', e.target.value)
                                                    }
                                                >
                                                    {geminiImageSizes.map((size) => (
                                                        <option key={size.value || 'default'} value={size.value}>
                                                            {size.label}
                                                        </option>
                                                    ))}
                                                </select>
                                            </div>

                                            <div className="modal-group">
                                                <label className="modal-label">{t('provider.imageQuantity')}</label>
                                                <input
                                                    aria-label={t('provider.imageQuantity')}
                                                    className="modal-input"
                                                    value="1"
                                                    disabled
                                                />
                                            </div>

                                            <div className="modal-group">
                                                <label className="modal-label">{t('provider.outputFormat')}</label>
                                                <select
                                                    aria-label={t('provider.outputFormat')}
                                                    className="modal-input modal-select"
                                                    value={
                                                        editingProvider.gemini_image_output_mime_type || 'image/jpeg'
                                                    }
                                                    onChange={(e) =>
                                                        handleProviderChange(
                                                            'gemini_image_output_mime_type',
                                                            e.target.value
                                                        )
                                                    }
                                                >
                                                    {GEMINI_IMAGE_OUTPUT_MIME_TYPES.map((mime) => (
                                                        <option
                                                            key={mime.value}
                                                            value={mime.value}
                                                            disabled={mime.value !== 'image/jpeg'}
                                                        >
                                                            {mime.value === 'image/jpeg'
                                                                ? mime.label
                                                                : t('provider.imageLegacyPng')}
                                                        </option>
                                                    ))}
                                                </select>
                                            </div>
                                        </div>
                                    )}

                                    {editingProvider.type !== 'gemini_image' && (
                                        <SamplingParamsFields
                                            provider={editingProvider}
                                            onChange={handleProviderChange}
                                        />
                                    )}

                                    {(editingProvider.type === 'openai' ||
                                        editingProvider.type === 'openai_response') && (
                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.reasoningEffort')}</label>
                                            <CustomSelect
                                                value={editingProvider.reasoning_effort ?? ''}
                                                options={openAIReasoningEffortOptions}
                                                ariaLabel={t('provider.reasoningEffort')}
                                                onChange={(value) => handleProviderChange('reasoning_effort', value)}
                                            />
                                        </div>
                                    )}

                                    {editingProvider.type === 'gemini' && (
                                        <div className="provider-form-grid">
                                            <div className="modal-group">
                                                <label className="modal-label">
                                                    {t('provider.geminiThinkingMode')}
                                                </label>
                                                <CustomSelect
                                                    value={editingProvider.gemini_thinking_mode || 'none'}
                                                    options={geminiThinkingModes}
                                                    ariaLabel={t('provider.geminiThinkingMode')}
                                                    onChange={(value) =>
                                                        handleProviderChange('gemini_thinking_mode', value)
                                                    }
                                                />
                                            </div>

                                            <div className="modal-group">
                                                <label className="modal-label">
                                                    {t('provider.thinkingLevelBudget')}
                                                </label>
                                                {editingProvider.gemini_thinking_mode === 'thinking_level' && (
                                                    <select
                                                        aria-label={t('provider.thinkingLevelBudget')}
                                                        className="modal-input modal-select"
                                                        value={editingProvider.gemini_thinking_level}
                                                        onChange={(e) =>
                                                            handleProviderChange(
                                                                'gemini_thinking_level',
                                                                e.target.value
                                                            )
                                                        }
                                                    >
                                                        {geminiThinkingLevels.map((level) => (
                                                            <option key={level.value} value={level.value}>
                                                                {level.label}
                                                            </option>
                                                        ))}
                                                    </select>
                                                )}
                                                {editingProvider.gemini_thinking_mode === 'thinking_budget' && (
                                                    <NumericInput
                                                        aria-label={t('provider.thinkingLevelBudget')}
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
                                                        aria-label={t('provider.thinkingLevelBudget')}
                                                        type="text"
                                                        className="modal-input"
                                                        value={t('common.disabled')}
                                                        disabled
                                                    />
                                                )}
                                            </div>
                                        </div>
                                    )}

                                    {editingProvider.type === 'anthropic' && (
                                        <>
                                            <div className="modal-group">
                                                <label className="modal-label">
                                                    {t('memoryProvider.thinkingBudget')}
                                                </label>
                                                <NumericInput
                                                    aria-label={t('memoryProvider.thinkingBudget')}
                                                    className="modal-input"
                                                    min={0}
                                                    step={1}
                                                    value={editingProvider.thinking_budget}
                                                    parseAs="int"
                                                    onValueChange={(value) =>
                                                        handleProviderChange('thinking_budget', value)
                                                    }
                                                    placeholder="0"
                                                />
                                            </div>

                                            {renderSwitch('prompt_caching', t('provider.promptCaching'))}

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

                                    {editingProvider.type !== 'gemini_image' && (
                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.contextRounds')}</label>
                                            <NumericInput
                                                aria-label={t('provider.contextRounds')}
                                                className="modal-input"
                                                min={1}
                                                step={1}
                                                value={editingProvider.context_messages}
                                                parseAs="int"
                                                onValueChange={(value) =>
                                                    handleProviderChange('context_messages', value)
                                                }
                                                placeholder="64"
                                            />
                                        </div>
                                    )}
                                </section>

                                {editingProvider.type !== 'gemini_image' && (
                                    <section className="provider-form-section">
                                        <h4 className="provider-form-section__title">
                                            {t('settingsManagement.provider.capabilities')}
                                        </h4>
                                        <div className="provider-form-switches">
                                            {renderSwitch('stream', t('provider.streamingOutput'))}
                                            {renderSwitch('image_capable', t('provider.visionSupport'))}
                                            {editingProvider.type === 'gemini' &&
                                                renderSwitch(
                                                    'gemini_automatic_continuation',
                                                    t('provider.geminiAutomaticContinuation'),
                                                    t('provider.geminiAutomaticContinuationHint')
                                                )}
                                        </div>
                                    </section>
                                )}
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
                    </ModalOverlay>
                )}
            </AnimatePresence>
        </motion.div>
    )
}

export default ProviderSettings
