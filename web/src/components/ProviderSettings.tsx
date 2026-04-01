import { useState, useEffect } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import type { Provider, ProviderType } from '../types/chat'
import { getProviders, addProvider, updateProvider, deleteProvider, setActiveProvider } from '../services/api'
import {
    getProviderTypesAll,
    getOpenAIReasoningEffortOptions,
    getGeminiThinkingModes,
    getGeminiThinkingLevels,
    GEMINI_IMAGE_ASPECT_RATIOS,
    getGeminiImageSizes,
    GEMINI_IMAGE_OUTPUT_MIME_TYPES,
    getGeminiThinkingBudgetRange,
    clampGeminiThinkingBudget,
    clampGeminiImageNumberOfImages,
    maskApiKey,
    CustomSelect,
    ModelSelect,
} from './provider'
import { NumericInput } from './NumericInput'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
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
        top_p: 1,
        thinking_budget: 0,
        reasoning_effort: '',
        gemini_thinking_mode: 'none',
        gemini_thinking_level: 'low',
        gemini_thinking_budget: 128,
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
            reasoning_effort: provider.reasoning_effort ?? '',
            gemini_thinking_mode: provider.gemini_thinking_mode || 'none',
            gemini_thinking_level: provider.gemini_thinking_level || 'low',
            gemini_thinking_budget: provider.gemini_thinking_budget || 128,
            gemini_image_aspect_ratio: provider.gemini_image_aspect_ratio || '1:1',
            gemini_image_size: provider.gemini_image_size || '',
            gemini_image_number_of_images: provider.gemini_image_number_of_images ?? 1,
            gemini_image_output_mime_type: provider.gemini_image_output_mime_type || 'image/jpeg',
            temperature: provider.type === 'anthropic' ? 1 : provider.temperature,
            top_p: provider.type === 'anthropic' ? 0 : provider.top_p,
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
                temperature: nextType === 'anthropic' ? 1 : editingProvider.temperature,
                top_p: nextType === 'anthropic' ? 0 : editingProvider.top_p,
            }
            if (nextType === 'gemini_image') {
                nextProvider.gemini_image_aspect_ratio = nextProvider.gemini_image_aspect_ratio || '1:1'
                nextProvider.gemini_image_size = nextProvider.gemini_image_size || ''
                nextProvider.gemini_image_number_of_images = clampGeminiImageNumberOfImages(
                    nextProvider.gemini_image_number_of_images ?? 1
                )
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

        if (field === 'gemini_image_number_of_images') {
            const nextNumber = clampGeminiImageNumberOfImages(Number(value) || 0)
            setEditingProvider({
                ...editingProvider,
                gemini_image_number_of_images: nextNumber,
            })
            return
        }

        setEditingProvider({ ...editingProvider, [field]: value })
    }

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
                <div className="provider-settings-title">{t('provider.title')}</div>
                <button className="header-add-button" onClick={handleAddNew} title={t('provider.addProvider')}>
                    <svg viewBox="0 0 24 24">
                        <path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z" />
                    </svg>
                </button>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : (
                <div className="provider-settings-content">
                    <div className="provider-cards">
                        {providers.map((provider) => {
                            const isActive = provider.id === activeProviderId
                            const isChatSelectable = provider.type !== 'gemini_image'
                            return (
                                <div
                                    key={provider.id}
                                    className={`provider-card ${isActive ? 'active' : 'inactive'}`}
                                    onClick={() => {
                                        if (isActive) return
                                        if (!isChatSelectable) {
                                            showToast(t('provider.imageOnly'), 'info')
                                            return
                                        }
                                        handleSetActive(provider.id)
                                    }}
                                >
                                    <div className="provider-card-header">
                                        <div className="provider-card-id">{provider.id}</div>
                                        {isActive && (
                                            <span className="active-indicator">{t('imageProvider.inUse')}</span>
                                        )}
                                    </div>
                                    <div className="provider-card-body">
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">{t('provider.type')}</span>
                                            <span className="provider-card-value type">
                                                {providerTypesAll.find((type) => type.value === provider.type)?.label ||
                                                    provider.type ||
                                                    t('provider.openaiCompatible')}
                                            </span>
                                        </div>
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">URL</span>
                                            <span className="provider-card-value">{provider.base_url}</span>
                                        </div>
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">Key</span>
                                            <span className="provider-card-value masked">
                                                {maskApiKey(provider.api_key)}
                                            </span>
                                        </div>
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">{t('provider.model')}</span>
                                            <span className="provider-card-value model">{provider.model}</span>
                                        </div>
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">{t('provider.temperature')}</span>
                                            <span className="provider-card-value">{provider.temperature}</span>
                                        </div>
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">Top P</span>
                                            <span className="provider-card-value">{provider.top_p}</span>
                                        </div>
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">{t('provider.context')}</span>
                                            <span className="provider-card-value">
                                                {provider.context_messages} {t('provider.roundUnit')}
                                            </span>
                                        </div>
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">{t('provider.streaming')}</span>
                                            <span
                                                className={`provider-card-value ${provider.stream ? 'stream-on' : 'stream-off'}`}
                                            >
                                                {provider.stream ? t('common.enable') : t('common.disable')}
                                            </span>
                                        </div>
                                        <div className="provider-card-row">
                                            <span className="provider-card-label">{t('provider.vision')}</span>
                                            <span
                                                className={`provider-card-value ${provider.image_capable ? 'vision-on' : 'vision-off'}`}
                                            >
                                                {provider.image_capable
                                                    ? t('common.supported')
                                                    : t('common.notSupported')}
                                            </span>
                                        </div>
                                    </div>
                                    <div className="provider-card-actions">
                                        <button
                                            className="card-action-btn edit"
                                            onClick={(e) => {
                                                e.stopPropagation()
                                                handleEditProvider(provider)
                                            }}
                                        >
                                            <svg viewBox="0 0 24 24">
                                                <path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34c-.39-.39-1.02-.39-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z" />
                                            </svg>
                                            {t('common.edit')}
                                        </button>
                                        <button
                                            className="card-action-btn delete"
                                            onClick={(e) => {
                                                e.stopPropagation()
                                                handleDeleteProvider(provider.id)
                                            }}
                                        >
                                            <svg viewBox="0 0 24 24">
                                                <path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z" />
                                            </svg>
                                            {t('common.delete')}
                                        </button>
                                    </div>
                                </div>
                            )
                        })}
                    </div>
                </div>
            )}

            {/* 悬浮编辑/新增卡片 */}
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
                                <h3>{isAddingNew ? t('provider.addProvider') : t('common.edit')}</h3>
                                <button className="modal-close" onClick={handleCloseModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="modal-body">
                                <div className="modal-group">
                                    <label className="modal-label">{t('provider.id')}</label>
                                    <input
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
                                        type="text"
                                        className="modal-input"
                                        value={editingProvider.name}
                                        onChange={(e) => handleProviderChange('name', e.target.value)}
                                        placeholder="OpenAI"
                                    />
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

                                <div className="modal-group">
                                    <label className="modal-label">{t('provider.apiUrl')}</label>
                                    <input
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
                                        type="password"
                                        className="modal-input"
                                        value={editingProvider.api_key}
                                        onChange={(e) => handleProviderChange('api_key', e.target.value)}
                                        placeholder={isAddingNew ? 'sk-...' : t('memoryProvider.apiKeyHint')}
                                    />
                                </div>

                                <div className="modal-group">
                                    <label className="modal-label">{t('provider.model')}</label>
                                    <ModelSelect
                                        value={editingProvider.model}
                                        providerId={editingProvider.id}
                                        providerType={editingProvider.type}
                                        baseUrl={editingProvider.base_url}
                                        apiKey={editingProvider.api_key}
                                        isNewProvider={isAddingNew}
                                        placeholder={
                                            editingProvider.type === 'gemini_image'
                                                ? 'nano banana / nanobanana Pro'
                                                : 'gpt-4'
                                        }
                                        onChange={(value) => handleProviderChange('model', value)}
                                        onError={(message) => showToast(message, 'error')}
                                    />
                                </div>

                                {editingProvider.type === 'gemini_image' && (
                                    <>
                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.imageAspectRatio')}</label>
                                            <select
                                                className="modal-input modal-select"
                                                value={editingProvider.gemini_image_aspect_ratio || '1:1'}
                                                onChange={(e) =>
                                                    handleProviderChange('gemini_image_aspect_ratio', e.target.value)
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
                                            <NumericInput
                                                className="modal-input"
                                                min={1}
                                                max={8}
                                                step={1}
                                                value={editingProvider.gemini_image_number_of_images ?? 1}
                                                parseAs="int"
                                                onValueChange={(value) =>
                                                    handleProviderChange('gemini_image_number_of_images', value)
                                                }
                                                placeholder="1"
                                            />
                                        </div>

                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.outputFormat')}</label>
                                            <select
                                                className="modal-input modal-select"
                                                value={editingProvider.gemini_image_output_mime_type || 'image/jpeg'}
                                                onChange={(e) =>
                                                    handleProviderChange(
                                                        'gemini_image_output_mime_type',
                                                        e.target.value
                                                    )
                                                }
                                            >
                                                {GEMINI_IMAGE_OUTPUT_MIME_TYPES.map((mime) => (
                                                    <option key={mime.value} value={mime.value}>
                                                        {mime.label}
                                                    </option>
                                                ))}
                                            </select>
                                        </div>
                                    </>
                                )}

                                {editingProvider.type !== 'gemini_image' && (
                                    <div className="modal-group">
                                        <label className="modal-label">{t('provider.temperature')}</label>
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
                                )}

                                {editingProvider.type !== 'gemini_image' && (
                                    <div className="modal-group">
                                        <label className="modal-label">{t('provider.topP')}</label>
                                        <NumericInput
                                            className="modal-input"
                                            min={0}
                                            max={1}
                                            step={0.1}
                                            value={editingProvider.top_p}
                                            parseAs="float"
                                            onValueChange={(value) => handleProviderChange('top_p', value)}
                                            placeholder="1"
                                            disabled={editingProvider.type === 'anthropic'}
                                        />
                                    </div>
                                )}

                                {(editingProvider.type === 'openai' || editingProvider.type === 'openai_response') && (
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
                                    <>
                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.geminiThinkingMode')}</label>
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
                                            <label className="modal-label">{t('provider.thinkingLevelBudget')}</label>
                                            {editingProvider.gemini_thinking_mode === 'thinking_level' && (
                                                <select
                                                    className="modal-input modal-select"
                                                    value={editingProvider.gemini_thinking_level}
                                                    onChange={(e) =>
                                                        handleProviderChange('gemini_thinking_level', e.target.value)
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

                                {editingProvider.type !== 'gemini_image' && (
                                    <>
                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.contextRounds')}</label>
                                            <NumericInput
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

                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.streamingOutput')}</label>
                                            <div className="modal-toggle-wrapper">
                                                <label className="toggle-switch">
                                                    <input
                                                        type="checkbox"
                                                        checked={editingProvider.stream}
                                                        onChange={(e) =>
                                                            handleProviderChange('stream', e.target.checked)
                                                        }
                                                    />
                                                    <span className="toggle-slider"></span>
                                                </label>
                                                <span className="toggle-label">
                                                    {editingProvider.stream ? t('common.enable') : t('common.disable')}
                                                </span>
                                            </div>
                                        </div>

                                        <div className="modal-group">
                                            <label className="modal-label">{t('provider.visionSupport')}</label>
                                            <div className="modal-toggle-wrapper">
                                                <label className="toggle-switch">
                                                    <input
                                                        type="checkbox"
                                                        checked={editingProvider.image_capable}
                                                        onChange={(e) =>
                                                            handleProviderChange('image_capable', e.target.checked)
                                                        }
                                                    />
                                                    <span className="toggle-slider"></span>
                                                </label>
                                                <span className="toggle-label">
                                                    {editingProvider.image_capable
                                                        ? t('common.supported')
                                                        : t('common.notSupported')}
                                                </span>
                                            </div>
                                        </div>
                                    </>
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
                    </motion.div>
                )}
            </AnimatePresence>
        </motion.div>
    )
}

export default ProviderSettings
