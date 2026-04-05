import { useState, useEffect } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { getProviders, searchWeatherCities, updateConfig, updateSystemPrompt } from '../services/api'
import {
    memoryService,
    type MemoryExtractionPromptTemplate,
    type MemoryExtractionSettings,
} from '../services/memoryService'
import { ttsService, type TTSProviderConfig } from '../services/ttsService'
import { clawBotService, type ClawBotSettings } from '../services/clawbotService'
import { reminderService } from '../services/reminderService'
import { webSearchService, type WebSearchSettings } from '../services/webSearchService'
import { localeNames, type Locale } from '../i18n'
import type { Provider, WeatherCity } from '../types/chat'
import type { Reminder } from '../types/reminder'
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
import ClawBotSettingsPanel from './ClawBotSettings'
import WebSearchSettingsPanel from './WebSearchSettings'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import {
    getNotificationsEnabled,
    isNotificationSupported,
    requestNotificationPermission,
    setNotificationsEnabled,
} from '../utils/notifications'
import { centerModalVariants, overlayVariants } from '../utils/motion'
import {
    countEnabledToolToggles,
    createDefaultToolToggles,
    normalizeToolToggles,
    TOOL_CONTROL_DEFINITIONS,
} from '../constants/toolControls'
import ToolSettingsPanel from './ToolSettings'
import ReminderSettingsPanel from './ReminderSettings'
import './Settings.css'

interface SettingsProps {
    onBack: () => void
}

const DEFAULT_TIME_ZONE = 'Asia/Shanghai'

const Settings: React.FC<SettingsProps> = ({ onBack }) => {
    const { t, locale, setLocale } = useT()
    const { showToast } = useToast()
    const appVersion = __CORNERSTONE_VERSION__.trim() || 'dev'
    const [systemPrompt, setSystemPrompt] = useState('')
    const [editingPrompt, setEditingPrompt] = useState('')
    const [activeProviderName, setActiveProviderName] = useState('')
    const [imageProviderPreview, setImageProviderPreview] = useState<{ title: string; detail: string }>({
        title: '',
        detail: '',
    })
    const [memoryProvider, setMemoryProvider] = useState<Provider | null>(null)
    const [clawBotSettings, setClawBotSettings] = useState<ClawBotSettings | null>(null)
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
    const [showClawBotSettings, setShowClawBotSettings] = useState(false)
    const [webSearchSettings, setWebSearchSettings] = useState<WebSearchSettings | null>(null)
    const [showWebSearchSettings, setShowWebSearchSettings] = useState(false)
    const [toolToggles, setToolToggles] = useState<Record<string, boolean>>(() => createDefaultToolToggles())
    const [showToolSettings, setShowToolSettings] = useState(false)
    const [showReminderSettings, setShowReminderSettings] = useState(false)
    const [reminders, setReminders] = useState<Reminder[]>([])
    const [showPromptModal, setShowPromptModal] = useState(false)
    const [replyWaitConfig, setReplyWaitConfigState] = useState<ReplyWaitWindowConfig>(() => getReplyWaitWindowConfig())
    const [editingReplyWaitConfig, setEditingReplyWaitConfig] = useState<ReplyWaitWindowConfig>(() =>
        getReplyWaitWindowConfig()
    )
    const [showReplyWaitModal, setShowReplyWaitModal] = useState(false)
    const [savingReplyWaitConfig, setSavingReplyWaitConfig] = useState(false)
    const [timeZone, setTimeZone] = useState(DEFAULT_TIME_ZONE)
    const [editingTimeZone, setEditingTimeZone] = useState(DEFAULT_TIME_ZONE)
    const [showTimeZoneModal, setShowTimeZoneModal] = useState(false)
    const [savingTimeZone, setSavingTimeZone] = useState(false)
    const [defaultWeatherCity, setDefaultWeatherCity] = useState<WeatherCity | null>(null)
    const [showWeatherCityModal, setShowWeatherCityModal] = useState(false)
    const [weatherCityQuery, setWeatherCityQuery] = useState('')
    const [weatherCityResults, setWeatherCityResults] = useState<WeatherCity[]>([])
    const [weatherCitySearchError, setWeatherCitySearchError] = useState('')
    const [weatherCityLoading, setWeatherCityLoading] = useState(false)
    const [weatherCitySearched, setWeatherCitySearched] = useState(false)
    const [selectedWeatherCity, setSelectedWeatherCity] = useState<WeatherCity | null>(null)
    const [weatherCitySaving, setWeatherCitySaving] = useState(false)
    const [showLanguageModal, setShowLanguageModal] = useState(false)
    const [notificationsEnabled, setNotificationsEnabledState] = useState(() => getNotificationsEnabled())
    const [notificationsSupported, setNotificationsSupported] = useState(() => isNotificationSupported())
    const [notificationPermission, setNotificationPermission] = useState<NotificationPermission | 'unsupported'>(() =>
        isNotificationSupported() ? Notification.permission : 'unsupported'
    )

    useEffect(() => {
        loadData()
    }, [])

    useEffect(() => {
        if (!loading) {
            void loadData({ showLoading: false })
        }
    }, [locale])

    useEffect(() => {
        const supported = isNotificationSupported()
        setNotificationsSupported(supported)
        setNotificationPermission(supported ? Notification.permission : 'unsupported')
        setNotificationsEnabledState(getNotificationsEnabled())
    }, [])

    useEffect(() => {
        if (!showWeatherCityModal) {
            return
        }

        const keyword = weatherCityQuery.trim()
        if (keyword === '') {
            setWeatherCityResults([])
            setWeatherCitySearchError('')
            setWeatherCityLoading(false)
            setWeatherCitySearched(false)
            return
        }

        let cancelled = false
        const timer = window.setTimeout(() => {
            setWeatherCityLoading(true)
            setWeatherCitySearchError('')
            void (async () => {
                try {
                    const results = await searchWeatherCities(keyword)
                    if (cancelled) return
                    setWeatherCityResults(results)
                    setWeatherCitySearched(true)
                } catch (error) {
                    if (cancelled) return
                    const message = error instanceof Error ? error.message : t('service.searchWeatherCitiesFailed')
                    setWeatherCityResults([])
                    setWeatherCitySearchError(message)
                    setWeatherCitySearched(true)
                } finally {
                    if (!cancelled) {
                        setWeatherCityLoading(false)
                    }
                }
            })()
        }, 300)

        return () => {
            cancelled = true
            window.clearTimeout(timer)
        }
    }, [showWeatherCityModal, weatherCityQuery, t])

    const loadData = async ({ showLoading = true }: { showLoading?: boolean } = {}) => {
        if (showLoading) setLoading(true)
        const providersData = await getProviders()
        if (providersData) {
            setSystemPrompt(providersData.system_prompt)
            setTimeZone((providersData.time_zone || DEFAULT_TIME_ZONE).trim() || DEFAULT_TIME_ZONE)
            setDefaultWeatherCity(providersData.weather_default_city || null)
            setToolToggles(normalizeToolToggles(providersData.tool_toggles))
            if (
                typeof providersData.reply_wait_window_mode === 'string' ||
                typeof providersData.reply_wait_window_seconds === 'number'
            ) {
                setReplyWaitWindowConfig({
                    mode: providersData.reply_wait_window_mode === 'fixed' ? 'fixed' : 'sliding',
                    seconds:
                        typeof providersData.reply_wait_window_seconds === 'number'
                            ? providersData.reply_wait_window_seconds
                            : getReplyWaitWindowConfig().seconds,
                })
                const syncedReplyWaitConfig = getReplyWaitWindowConfig()
                setReplyWaitConfigState(syncedReplyWaitConfig)
                setEditingReplyWaitConfig(syncedReplyWaitConfig)
            }
            const activeProvider = providersData.providers.find((p) => p.id === providersData.active_provider_id)
            setActiveProviderName(activeProvider?.name || t('common.notSet'))
            const configuredImageProviderId = providersData.image_provider_id || ''
            const imageProviders = providersData.providers.filter((p) => p.type === 'gemini_image')
            if (configuredImageProviderId) {
                const selected = imageProviders.find((p) => p.id === configuredImageProviderId)
                setImageProviderPreview({
                    title: selected?.name || t('common.notConfigured'),
                    detail: selected?.model || '',
                })
            } else {
                const auto = imageProviders[0]
                if (auto) {
                    setImageProviderPreview({
                        title: t('imageProvider.autoSelect'),
                        detail: `${auto.name || auto.id}${auto.model ? ` · ${auto.model}` : ''}`,
                    })
                } else {
                    setImageProviderPreview({
                        title: t('common.notConfigured'),
                        detail: t('imageProvider.noProviders'),
                    })
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
        try {
            const settings = await clawBotService.getSettings()
            setClawBotSettings(settings)
        } catch {
            setClawBotSettings(null)
        }
        try {
            const settings = await webSearchService.getSettings()
            setWebSearchSettings(settings)
        } catch {
            setWebSearchSettings(null)
        }
        try {
            const reminderList = await reminderService.listReminders()
            setReminders(reminderList)
        } catch {
            setReminders([])
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

    const handleOpenTimeZoneModal = () => {
        setEditingTimeZone(timeZone || DEFAULT_TIME_ZONE)
        setSavingTimeZone(false)
        setShowTimeZoneModal(true)
    }

    const handleCloseTimeZoneModal = () => {
        setShowTimeZoneModal(false)
    }

    const handleOpenWeatherCityModal = () => {
        setSelectedWeatherCity(defaultWeatherCity)
        setWeatherCityQuery('')
        setWeatherCityResults([])
        setWeatherCitySearchError('')
        setWeatherCityLoading(false)
        setWeatherCitySearched(false)
        setWeatherCitySaving(false)
        setShowWeatherCityModal(true)
    }

    const handleCloseWeatherCityModal = () => {
        setShowWeatherCityModal(false)
    }

    const handleOpenLanguageModal = () => {
        setShowLanguageModal(true)
    }

    const handleCloseLanguageModal = () => {
        setShowLanguageModal(false)
    }

    const handleSelectLocale = (nextLocale: Locale) => {
        setLocale(nextLocale)
        handleCloseLanguageModal()
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
            showToast(t('settings.memoryExtractionRoundsSaved'), 'success')
            handleCloseMemoryExtractionRoundsModal()
        } catch (error) {
            const message = error instanceof Error ? error.message : t('common.saveFailed')
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
            showToast(t('settings.memoryRefreshIntervalSaved'), 'success')
            handleCloseMemoryRefreshIntervalModal()
        } catch (error) {
            const message = error instanceof Error ? error.message : t('common.saveFailed')
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
                const message = error instanceof Error ? error.message : t('common.loadFailed')
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
            showToast(t('settings.memoryExtractionPromptSaved'), 'success')
            handleCloseMemoryExtractionPromptModal()
        } catch (error) {
            const message = error instanceof Error ? error.message : t('common.saveFailed')
            showToast(message, 'error')
        } finally {
            setSavingMemoryExtractionPrompt(false)
        }
    }

    const handleSaveReplyWaitConfig = async () => {
        if (savingReplyWaitConfig) return
        setSavingReplyWaitConfig(true)
        try {
            const success = await updateConfig({
                reply_wait_window_mode: editingReplyWaitConfig.mode,
                reply_wait_window_seconds: editingReplyWaitConfig.seconds,
            })
            if (!success) {
                showToast(t('common.saveFailed'), 'error')
                return
            }

            setReplyWaitConfig(editingReplyWaitConfig)
            showToast(t('settings.replyWaitWindowSaved'), 'success')
            handleCloseReplyWaitModal()
        } finally {
            setSavingReplyWaitConfig(false)
        }
    }

    const handleSaveWeatherCity = async () => {
        if (weatherCitySaving || !selectedWeatherCity) return

        setWeatherCitySaving(true)
        try {
            const success = await updateConfig({ weather_default_city: selectedWeatherCity })
            if (!success) {
                showToast(t('common.saveFailed'), 'error')
                return
            }

            setDefaultWeatherCity(selectedWeatherCity)
            showToast(t('settings.defaultWeatherCitySaved'), 'success')
            handleCloseWeatherCityModal()
        } finally {
            setWeatherCitySaving(false)
        }
    }

    const handleSaveTimeZone = async () => {
        if (savingTimeZone) return

        setSavingTimeZone(true)
        try {
            const nextTimeZone = editingTimeZone.trim() || DEFAULT_TIME_ZONE
            const success = await updateConfig({ time_zone: nextTimeZone })
            if (!success) {
                showToast(t('common.saveFailed'), 'error')
                return
            }

            setTimeZone(nextTimeZone)
            showToast(t('settings.timeZoneSaved'), 'success')
            handleCloseTimeZoneModal()
        } finally {
            setSavingTimeZone(false)
        }
    }

    const handleSaveSystemPrompt = async () => {
        setSaving(true)
        const success = await updateSystemPrompt(editingPrompt)
        if (success) {
            setSystemPrompt(editingPrompt)
            showToast(t('settings.systemPromptSaved'), 'success')
            handleClosePromptModal()
        } else {
            showToast(t('common.saveFailed'), 'error')
        }
        setSaving(false)
    }

    const handleNotificationsToggle = async (enabled: boolean) => {
        if (!enabled) {
            setNotificationsEnabled(false)
            setNotificationsEnabledState(false)
            showToast(t('settings.notificationDisabled'), 'success')
            return
        }

        if (!isNotificationSupported()) {
            setNotificationsSupported(false)
            setNotificationPermission('unsupported')
            setNotificationsEnabled(false)
            setNotificationsEnabledState(false)
            showToast(t('settings.notificationNotSupported'), 'error')
            return
        }

        if (typeof window !== 'undefined' && window.isSecureContext === false && location.hostname !== 'localhost') {
            showToast(t('settings.notificationNeedsHTTPS'), 'error')
            return
        }

        const permission = await requestNotificationPermission()
        setNotificationPermission(permission)
        if (permission !== 'granted') {
            setNotificationsEnabled(false)
            setNotificationsEnabledState(false)
            showToast(
                permission === 'denied'
                    ? t('settings.notificationDeniedHint')
                    : t('settings.notificationPermissionHint'),
                'error'
            )
            return
        }

        setNotificationsEnabled(true)
        setNotificationsEnabledState(true)
        showToast(t('settings.notificationEnabled'), 'success')
    }

    const handleTTSEnabledChange = async (enabled: boolean) => {
        if (saving) return
        setSaving(true)
        try {
            const settings = await ttsService.updateTTSSettings({ enabled })
            setTTSEnabledState(settings.enabled)
            setTTSProvider(settings.provider)
            showToast(enabled ? t('settings.ttsEnabled') : t('settings.ttsDisabled'), 'success')
        } catch (error) {
            console.error('Failed to set tts enabled:', error)
            showToast(t('settings.settingFailed'), 'error')
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
            showToast(t('settings.ttsSaved'), 'success')
            handleCloseTTSProviderModal()
        } catch (error) {
            console.error('Failed to save tts provider:', error)
            showToast(t('common.saveFailed'), 'error')
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
            showToast(enabled ? t('settings.memoryEnabled') : t('settings.memoryDisabled'), 'success')
        } catch (error) {
            console.error('Failed to set memory enabled:', error)
            showToast(t('settings.settingFailed'), 'error')
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

    const handleClawBotSettingsBack = () => {
        setShowClawBotSettings(false)
    }

    const handleWebSearchSettingsBack = () => {
        setShowWebSearchSettings(false)
    }

    const handleToolSettingsBack = () => {
        setShowToolSettings(false)
    }

    const handleReminderSettingsBack = () => {
        setShowReminderSettings(false)
    }

    const getPromptPreview = () => {
        if (!systemPrompt) return t('common.notSet')
        if (systemPrompt.length <= 20) return systemPrompt
        return systemPrompt.substring(0, 20) + '...'
    }

    const getMemoryProviderPreview = () => {
        if (memoryProvider) {
            const name = memoryProvider.name || t('common.unnamed')
            const model = memoryProvider.model || t('settings.modelNotSet')
            return { title: name, detail: model }
        }
        if (activeProviderName) return { title: t('settings.followChatModel'), detail: activeProviderName }
        return { title: t('settings.followChatModel'), detail: t('common.default') }
    }

    const getTTSProviderPreview = () => {
        if (!ttsProvider) return { title: t('common.notConfigured'), detail: '' }
        const model = ttsProvider.model || ''
        const voiceId = ttsProvider.voice_setting?.voice_id || ''
        const detail = [model, voiceId].filter(Boolean).join(' · ')
        return { title: 'MiniMax', detail }
    }

    const getReplyWaitPreview = () => {
        return formatReplyWaitWindowConfig(replyWaitConfig)
    }

    const getTimeZonePreview = () => {
        const resolvedTimeZone = (timeZone || DEFAULT_TIME_ZONE).trim() || DEFAULT_TIME_ZONE

        try {
            const formatter = new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', {
                timeZone: resolvedTimeZone,
                year: 'numeric',
                month: '2-digit',
                day: '2-digit',
                hour: '2-digit',
                minute: '2-digit',
                second: '2-digit',
                hour12: false,
            })

            return {
                title: resolvedTimeZone,
                detail: formatter.format(new Date()),
            }
        } catch {
            return {
                title: resolvedTimeZone,
                detail: t('settings.timeZoneHint'),
            }
        }
    }

    const getDefaultWeatherCityPreview = () => {
        if (!defaultWeatherCity) {
            return { title: t('common.notSet'), detail: t('settings.defaultWeatherCityHint') }
        }
        return {
            title: defaultWeatherCity.name,
            detail: defaultWeatherCity.affiliation || defaultWeatherCity.location_key,
        }
    }

    const getMemoryExtractionRoundsPreview = () => {
        const rounds = memoryExtractionRounds || memoryExtractionSettings?.rounds || 5
        return t('settings.roundsPreview', { count: rounds })
    }

    const getMemoryExtractionRoundsDetail = () => {
        const maxRounds = memoryExtractionMaxRounds || memoryExtractionSettings?.max_rounds || 1
        if (memoryExtractionProviderName) {
            return t('settings.maxRoundsWithProvider', { max: maxRounds, provider: memoryExtractionProviderName })
        }
        return t('settings.maxRounds', { max: maxRounds })
    }

    const getMemoryRefreshIntervalPreview = () => {
        const interval = memoryRefreshInterval || memoryExtractionSettings?.refresh_interval || 5
        return t('settings.roundsPreview', { count: interval })
    }

    const getMemoryRefreshIntervalDetail = () => {
        const maxInterval = memoryRefreshMaxInterval || memoryExtractionSettings?.max_refresh_interval || 99
        return t('settings.maxRounds', { max: maxInterval })
    }

    const getClawBotPreview = () => {
        if (!clawBotSettings) return { title: t('common.notConfigured'), detail: '' }
        const statusMap: Record<string, string> = {
            disabled: t('clawBot.disabled'),
            missing_token: t('clawBot.missingToken'),
            running: t('clawBot.running'),
            error: t('clawBot.error'),
            stopped: t('clawBot.stopped'),
        }
        const title = statusMap[clawBotSettings.status] || clawBotSettings.status || t('common.notConfigured')
        const detail = clawBotSettings.prompt_name
            ? `${t('settings.persona')}：${clawBotSettings.prompt_name}`
            : clawBotSettings.ilink_user_id
              ? `${t('settings.account')}：${clawBotSettings.ilink_user_id}`
              : clawBotSettings.has_bot_token
                ? t('settings.hasBotToken')
                : t('settings.noBotToken')
        return { title, detail }
    }

    const getWebSearchPreview = () => {
        if (!webSearchSettings) return { title: t('common.notConfigured'), detail: '' }
        const activeId = (webSearchSettings.active_provider_id || '').trim()
        if (!activeId) return { title: t('common.notConfigured'), detail: '' }
        const info = (webSearchSettings.available_providers || []).find((p) => p.id === activeId)
        const title = info?.name || activeId
        const detailParts = []
        if (webSearchSettings.max_results) {
            detailParts.push(`${t('settings.webSearchMaxResults')}: ${webSearchSettings.max_results}`)
        }
        if (webSearchSettings.fetch_results && webSearchSettings.fetch_results !== webSearchSettings.max_results) {
            detailParts.push(`${t('settings.webSearchFetchResults')}: ${webSearchSettings.fetch_results}`)
        }
        const detail = detailParts.join(' · ')
        return { title, detail }
    }

    const getToolControlPreview = () => {
        const enabled = countEnabledToolToggles(toolToggles)
        return {
            title: t('settings.toolControlSummary', {
                enabled,
                total: TOOL_CONTROL_DEFINITIONS.length,
            }),
            detail: t('settings.toolControlHint'),
        }
    }

    const getReminderPreview = () => {
        if (reminders.length === 0) {
            return {
                title: t('settings.noReminders'),
                detail: t('settings.reminderManagerHint'),
            }
        }

        const pendingReminders = reminders.filter((item) => item.status === 'pending')
        const nextPending = [...pendingReminders].sort(
            (left, right) => new Date(left.due_at).getTime() - new Date(right.due_at).getTime()
        )[0]

        return {
            title: t('settings.reminderSummary', {
                pending: pendingReminders.length,
                total: reminders.length,
            }),
            detail: nextPending ? `${nextPending.title} · ${nextPending.due_at}` : t('settings.reminderNoPending'),
        }
    }

    const memoryProviderPreview = getMemoryProviderPreview()
    const ttsProviderPreview = getTTSProviderPreview()
    const clawBotPreview = getClawBotPreview()
    const webSearchPreview = getWebSearchPreview()
    const toolControlPreview = getToolControlPreview()
    const reminderPreview = getReminderPreview()
    const timeZonePreview = getTimeZonePreview()
    const defaultWeatherCityPreview = getDefaultWeatherCityPreview()
    const localeOptions = Object.entries(localeNames) as [Locale, string][]

    return (
        <div className="settings">
            <div className="settings-header">
                <button className="back-button" onClick={onBack}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="settings-title">{t('settings.title')}</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="settings-loading">{t('common.loading')}</div>
            ) : (
                <div className="settings-content">
                    {/* 供应商设置入口 */}
                    <div className="settings-section">
                        <h3>{t('settings.providers')}</h3>
                        <button className="settings-entry-btn" onClick={() => setShowProviderSettings(true)}>
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.currentProvider')}</span>
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
                                <span className="settings-entry-label">{t('settings.imageProvider')}</span>
                                <span className="settings-entry-value">{imageProviderPreview.title}</span>
                                {imageProviderPreview.detail && (
                                    <span className="settings-entry-subvalue">{imageProviderPreview.detail}</span>
                                )}
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>

                        <button
                            className="settings-entry-btn"
                            onClick={() => setShowWebSearchSettings(true)}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.webSearch')}</span>
                                <span className="settings-entry-value">{webSearchPreview.title}</span>
                                {webSearchPreview.detail && (
                                    <span className="settings-entry-subvalue">{webSearchPreview.detail}</span>
                                )}
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                    </div>

                    <div className="settings-section">
                        <h3>{t('settings.tools')}</h3>
                        <button className="settings-entry-btn" onClick={() => setShowToolSettings(true)}>
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.tools')}</span>
                                <span className="settings-entry-value">{toolControlPreview.title}</span>
                                <span className="settings-entry-subvalue">{toolControlPreview.detail}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>

                        <button
                            className="settings-entry-btn"
                            onClick={() => setShowReminderSettings(true)}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.reminders')}</span>
                                <span className="settings-entry-value">{reminderPreview.title}</span>
                                <span className="settings-entry-subvalue">{reminderPreview.detail}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                    </div>

                    <div className="settings-section">
                        <h3>{t('settings.channels')}</h3>
                        <button className="settings-entry-btn" onClick={() => setShowClawBotSettings(true)}>
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.wechatClawBot')}</span>
                                <span className="settings-entry-value">{clawBotPreview.title}</span>
                                {clawBotPreview.detail && (
                                    <span className="settings-entry-subvalue">{clawBotPreview.detail}</span>
                                )}
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                    </div>

                    {/* 全局设置 */}
                    <div className="settings-section">
                        <h3>{t('settings.globalSettings')}</h3>
                        <button className="settings-entry-btn" onClick={handleOpenPromptModal}>
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.defaultSystemPrompt')}</span>
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
                                <span className="settings-entry-label">{t('settings.replyWaitWindow')}</span>
                                <span className="settings-entry-value">{getReplyWaitPreview()}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenTimeZoneModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.timeZone')}</span>
                                <span className="settings-entry-value">{timeZonePreview.title}</span>
                                {timeZonePreview.detail && (
                                    <span className="settings-entry-subvalue">{timeZonePreview.detail}</span>
                                )}
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenWeatherCityModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.defaultWeatherCity')}</span>
                                <span className="settings-entry-value">{defaultWeatherCityPreview.title}</span>
                                {defaultWeatherCityPreview.detail && (
                                    <span className="settings-entry-subvalue">{defaultWeatherCityPreview.detail}</span>
                                )}
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>

                        <div className="settings-group" style={{ marginTop: 12 }}>
                            <label className="settings-label">{t('settings.systemNotifications')}</label>
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
                                            ? t('common.enable')
                                            : notificationPermission === 'denied'
                                              ? t('common.denied')
                                              : t('common.disable')
                                        : t('common.notSupported')}
                                </span>
                            </div>
                            <p className="prompt-modal-hint memory-toggle-hint">{t('settings.notifyWhenNotInChat')}</p>
                        </div>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenLanguageModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.language')}</span>
                                <span className="settings-entry-value">{localeNames[locale]}</span>
                                <span className="settings-entry-subvalue">{t('settings.languageHint')}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                    </div>

                    {/* 语音设置 */}
                    <div className="settings-section">
                        <h3>{t('settings.voice')}</h3>

                        <div className="settings-group">
                            <label className="settings-label">{t('settings.tts')}</label>
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
                                <span className="toggle-label">
                                    {ttsEnabled ? t('common.enable') : t('common.disable')}
                                </span>
                            </div>
                            <p className="prompt-modal-hint memory-toggle-hint">{t('settings.ttsHint')}</p>
                        </div>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenTTSProviderModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.ttsProvider')}</span>
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
                        <h3>{t('settings.longTermMemory')}</h3>

                        <p className="prompt-modal-hint">
                            {t('settings.memoryHint', { rounds: memoryExtractionRounds || 5 })}
                        </p>

                        <div className="settings-group">
                            <label className="settings-label">{t('settings.memoryFunction')}</label>
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
                                <span className="toggle-label">
                                    {memoryEnabled ? t('common.enable') : t('common.disable')}
                                </span>
                            </div>
                            <p className="prompt-modal-hint memory-toggle-hint">{t('settings.memoryDisableHint')}</p>
                        </div>

                        <button
                            className="settings-entry-btn"
                            onClick={() => setShowMemoryProviderSettings(true)}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.memoryProvider')}</span>
                                <span className="settings-entry-value">{memoryProviderPreview.title}</span>
                                <span className="settings-entry-subvalue">{memoryProviderPreview.detail}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                        <p className="prompt-modal-hint memory-provider-hint">{t('settings.memoryProviderHint')}</p>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenMemoryExtractionRoundsModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.memoryExtractionRounds')}</span>
                                <span className="settings-entry-value">{getMemoryExtractionRoundsPreview()}</span>
                                <span className="settings-entry-subvalue">{getMemoryExtractionRoundsDetail()}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                        <p className="prompt-modal-hint memory-provider-hint">
                            {t('settings.memoryExtractionRoundsHint')}
                        </p>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenMemoryRefreshIntervalModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.memoryRefreshInterval')}</span>
                                <span className="settings-entry-value">{getMemoryRefreshIntervalPreview()}</span>
                                <span className="settings-entry-subvalue">{getMemoryRefreshIntervalDetail()}</span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                        <p className="prompt-modal-hint memory-provider-hint">
                            {t('settings.memoryRefreshIntervalHint')}
                        </p>

                        <button
                            className="settings-entry-btn"
                            onClick={handleOpenMemoryExtractionPromptModal}
                            style={{ marginTop: 12 }}
                        >
                            <div className="settings-entry-info">
                                <span className="settings-entry-label">{t('settings.memoryExtractionPrompt')}</span>
                                <span className="settings-entry-value">{t('common.edit')}</span>
                                <span className="settings-entry-subvalue">
                                    {t('settings.memoryExtractionPromptSupport')}
                                </span>
                            </div>
                            <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                            </svg>
                        </button>
                    </div>

                    <div className="settings-footer">
                        <span className="settings-footer-label">{t('settings.currentVersion')}</span>
                        <span className="settings-footer-value">{appVersion}</span>
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

            <AnimatePresence onExitComplete={() => void loadData({ showLoading: false })}>
                {showClawBotSettings && <ClawBotSettingsPanel onBack={handleClawBotSettingsBack} />}
            </AnimatePresence>

            <AnimatePresence onExitComplete={() => void loadData({ showLoading: false })}>
                {showWebSearchSettings && <WebSearchSettingsPanel onBack={handleWebSearchSettingsBack} />}
            </AnimatePresence>

            <AnimatePresence onExitComplete={() => void loadData({ showLoading: false })}>
                {showToolSettings && <ToolSettingsPanel onBack={handleToolSettingsBack} />}
            </AnimatePresence>

            <AnimatePresence onExitComplete={() => void loadData({ showLoading: false })}>
                {showReminderSettings && <ReminderSettingsPanel onBack={handleReminderSettingsBack} />}
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
                                <h3>{t('settings.editSystemPrompt')}</h3>
                                <button className="prompt-modal-close" onClick={handleClosePromptModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.systemPromptHint')}</p>
                                <textarea
                                    className="prompt-modal-textarea"
                                    value={editingPrompt}
                                    onChange={(e) => setEditingPrompt(e.target.value)}
                                    placeholder={t('settings.systemPromptPlaceholder')}
                                    rows={8}
                                />
                            </div>

                            <div className="prompt-modal-footer">
                                <button className="prompt-modal-btn cancel" onClick={handleClosePromptModal}>
                                    {t('common.cancel')}
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveSystemPrompt}
                                    disabled={saving}
                                >
                                    {saving ? t('common.saving') : t('common.save')}
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
                                <h3>{t('settings.replyWaitWindow')}</h3>
                                <button className="prompt-modal-close" onClick={handleCloseReplyWaitModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.replyWaitWindowHint')}</p>

                                <div className="settings-group">
                                    <label className="settings-label">{t('settings.mergeMode')}</label>
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
                                        <option value="fixed">{t('settings.fixedTime')}</option>
                                        <option value="sliding">{t('settings.slidingTime')}</option>
                                    </select>
                                </div>

                                <div className="settings-group">
                                    <label className="settings-label">{t('settings.waitSeconds')}</label>
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

                                <p className="prompt-modal-hint">{t('settings.zeroSecondsHint')}</p>
                            </div>

                            <div className="prompt-modal-footer">
                                <button className="prompt-modal-btn cancel" onClick={handleCloseReplyWaitModal}>
                                    {t('common.cancel')}
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={() => void handleSaveReplyWaitConfig()}
                                    disabled={savingReplyWaitConfig}
                                >
                                    {t('common.save')}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showTimeZoneModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseTimeZoneModal}
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
                                <h3>{t('settings.timeZone')}</h3>
                                <button className="prompt-modal-close" onClick={handleCloseTimeZoneModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.timeZoneHint')}</p>
                                <div className="settings-group">
                                    <input
                                        className="settings-input"
                                        value={editingTimeZone}
                                        onChange={(e) => setEditingTimeZone(e.target.value)}
                                        placeholder={t('settings.timeZonePlaceholder')}
                                    />
                                </div>
                                <p className="prompt-modal-hint">{t('settings.timeZoneExample')}</p>
                            </div>

                            <div className="prompt-modal-footer">
                                <button className="prompt-modal-btn cancel" onClick={handleCloseTimeZoneModal}>
                                    {t('common.cancel')}
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={() => void handleSaveTimeZone()}
                                    disabled={savingTimeZone}
                                >
                                    {savingTimeZone ? t('common.saving') : t('common.save')}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showWeatherCityModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseWeatherCityModal}
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
                                <h3>{t('settings.defaultWeatherCity')}</h3>
                                <button className="prompt-modal-close" onClick={handleCloseWeatherCityModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.defaultWeatherCityHint')}</p>
                                <div className="settings-group">
                                    <input
                                        className="settings-input"
                                        value={weatherCityQuery}
                                        onChange={(e) => setWeatherCityQuery(e.target.value)}
                                        placeholder={t('settings.weatherCitySearchPlaceholder')}
                                    />
                                </div>

                                <div className="weather-city-current">
                                    <span className="weather-city-current-label">
                                        {t('settings.selectedWeatherCity')}
                                    </span>
                                    <span className="weather-city-current-value">
                                        {selectedWeatherCity?.name || t('common.notSet')}
                                    </span>
                                    {(selectedWeatherCity?.affiliation || selectedWeatherCity?.location_key) && (
                                        <span className="weather-city-current-detail">
                                            {selectedWeatherCity?.affiliation || selectedWeatherCity?.location_key}
                                        </span>
                                    )}
                                </div>

                                {weatherCityLoading ? (
                                    <p className="weather-city-status">{t('common.loading')}</p>
                                ) : weatherCitySearchError ? (
                                    <p className="weather-city-status error">{weatherCitySearchError}</p>
                                ) : weatherCityQuery.trim() === '' ? (
                                    <p className="weather-city-status">{t('settings.weatherCitySearchHint')}</p>
                                ) : weatherCityResults.length === 0 && weatherCitySearched ? (
                                    <p className="weather-city-status">{t('settings.weatherCityNoResults')}</p>
                                ) : (
                                    <div className="weather-city-results">
                                        {weatherCityResults.map((city) => {
                                            const isSelected = selectedWeatherCity?.location_key === city.location_key
                                            return (
                                                <button
                                                    key={city.location_key}
                                                    type="button"
                                                    className={`weather-city-item${isSelected ? ' selected' : ''}`}
                                                    onClick={() => setSelectedWeatherCity(city)}
                                                >
                                                    <span className="weather-city-item-name">{city.name}</span>
                                                    <span className="weather-city-item-affiliation">
                                                        {city.affiliation || city.location_key}
                                                    </span>
                                                </button>
                                            )
                                        })}
                                    </div>
                                )}
                            </div>

                            <div className="prompt-modal-footer">
                                <button className="prompt-modal-btn cancel" onClick={handleCloseWeatherCityModal}>
                                    {t('common.cancel')}
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={() => void handleSaveWeatherCity()}
                                    disabled={weatherCitySaving || !selectedWeatherCity}
                                >
                                    {weatherCitySaving ? t('common.saving') : t('common.save')}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showLanguageModal && (
                    <motion.div
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onClick={handleCloseLanguageModal}
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
                                <h3>{t('settings.language')}</h3>
                                <button className="prompt-modal-close" onClick={handleCloseLanguageModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.languageHint')}</p>
                                <div className="settings-option-list">
                                    {localeOptions.map(([value, label]) => {
                                        const active = value === locale
                                        return (
                                            <button
                                                key={value}
                                                type="button"
                                                className={`settings-option-item${active ? ' selected' : ''}`}
                                                onClick={() => handleSelectLocale(value)}
                                            >
                                                <span className="settings-option-title">{label}</span>
                                                {active && (
                                                    <svg className="settings-option-check" viewBox="0 0 24 24">
                                                        <path d="M9 16.17l-3.88-3.88L4 13.41 9 18.41 20 7.41 18.59 6z" />
                                                    </svg>
                                                )}
                                            </button>
                                        )
                                    })}
                                </div>
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
                                <h3>{t('settings.ttsProvider')}</h3>
                                <button className="prompt-modal-close" onClick={handleCloseTTSProviderModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.ttsProviderHint')}</p>

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
                                    <p className="prompt-modal-hint">{t('settings.ttsApiKeyHint')}</p>
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
                                    {t('common.cancel')}
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveTTSProvider}
                                    disabled={saving}
                                >
                                    {saving ? t('common.saving') : t('common.save')}
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
                                <h3>{t('settings.memoryExtractionRounds')}</h3>
                                <button className="prompt-modal-close" onClick={handleCloseMemoryExtractionRoundsModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.memoryExtractionRoundsModalHint')}</p>
                                <div className="settings-group">
                                    <label className="settings-label">{t('settings.rounds')}</label>
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
                                    {t('common.cancel')}
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveMemoryExtractionRounds}
                                    disabled={savingMemoryExtractionRounds}
                                >
                                    {savingMemoryExtractionRounds ? t('common.saving') : t('common.save')}
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
                                <h3>{t('settings.memoryRefreshInterval')}</h3>
                                <button className="prompt-modal-close" onClick={handleCloseMemoryRefreshIntervalModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.memoryRefreshIntervalModalHint')}</p>
                                <div className="settings-group">
                                    <label className="settings-label">{t('settings.intervalRounds')}</label>
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
                                    {t('common.cancel')}
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveMemoryRefreshInterval}
                                    disabled={savingMemoryRefreshInterval}
                                >
                                    {savingMemoryRefreshInterval ? t('common.saving') : t('common.save')}
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
                                <h3>{t('settings.memoryExtractionPrompt')}</h3>
                                <button className="prompt-modal-close" onClick={handleCloseMemoryExtractionPromptModal}>
                                    <svg viewBox="0 0 24 24">
                                        <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                    </svg>
                                </button>
                            </div>

                            <div className="prompt-modal-body">
                                <p className="prompt-modal-hint">{t('settings.memoryExtractionPromptHint')}</p>

                                <div className="memory-extraction-template-actions">
                                    <button
                                        className="action-button edit"
                                        onClick={() => setEditingMemoryExtractionPrompt(defaultMemoryExtractionPrompt)}
                                        disabled={loadingMemoryExtractionPrompt || defaultMemoryExtractionPrompt === ''}
                                        type="button"
                                    >
                                        {t('settings.restoreDefault')}
                                    </button>
                                </div>

                                <textarea
                                    className="prompt-modal-textarea"
                                    value={editingMemoryExtractionPrompt}
                                    onChange={(e) => setEditingMemoryExtractionPrompt(e.target.value)}
                                    placeholder={
                                        loadingMemoryExtractionPrompt
                                            ? t('common.loading')
                                            : t('settings.memoryExtractionPromptPlaceholder')
                                    }
                                    rows={12}
                                    disabled={loadingMemoryExtractionPrompt}
                                />
                            </div>

                            <div className="prompt-modal-footer">
                                <button
                                    className="prompt-modal-btn cancel"
                                    onClick={handleCloseMemoryExtractionPromptModal}
                                >
                                    {t('common.cancel')}
                                </button>
                                <button
                                    className="prompt-modal-btn save"
                                    onClick={handleSaveMemoryExtractionPrompt}
                                    disabled={savingMemoryExtractionPrompt || loadingMemoryExtractionPrompt}
                                >
                                    {savingMemoryExtractionPrompt ? t('common.saving') : t('common.save')}
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
