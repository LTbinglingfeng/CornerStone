import { useEffect, useMemo, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import {
    cornerstoneWebSearchService,
    type CornerstoneWebSearchProviderConfig,
    type CornerstoneWebSearchProviderInfo,
    type CornerstoneWebSearchSettings,
} from '../services/cornerstoneWebSearchService'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { Group, Toggle } from './ui'
import { SettingsField } from './SettingsField'
import { NumericInput } from './NumericInput'
import { ModalOverlay } from './ModalOverlay'
import { CustomSelect, type SelectOption } from './provider'
import { drawerVariants, overlayVariants } from '../utils/motion'
import './ProviderSettings.css'

interface CornerstoneWebSearchSettingsProps {
    onBack: () => void
}

const splitExcludeDomains = (raw: string): string[] =>
    raw
        .split(/[\n,]/g)
        .map((part) => part.trim())
        .filter((part) => part !== '')

const ZHIPU_SEARCH_ENGINE_OPTIONS: SelectOption[] = [
    { value: 'search_std', label: 'search_std' },
    { value: 'search_pro', label: 'search_pro' },
    { value: 'search_pro_sogou', label: 'search_pro_sogou' },
    { value: 'search_pro_quark', label: 'search_pro_quark' },
]

const CornerstoneWebSearchSettingsPanel: React.FC<CornerstoneWebSearchSettingsProps> = ({ onBack }) => {
    const { t } = useT()
    const { showToast } = useToast()

    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)

    const [availableProviders, setAvailableProviders] = useState<CornerstoneWebSearchProviderInfo[]>([])
    const [providers, setProviders] = useState<Record<string, CornerstoneWebSearchProviderConfig>>({})

    const [activeProviderId, setActiveProviderId] = useState('')
    const [maxResults, setMaxResults] = useState(5)
    const [fetchResults, setFetchResults] = useState(5)
    const [excludeDomainsText, setExcludeDomainsText] = useState('')
    const [searchWithTime, setSearchWithTime] = useState(false)
    const [timeoutSeconds, setTimeoutSeconds] = useState(20)
    const [searchEngine, setSearchEngine] = useState('search_std')

    const [apiHost, setApiHost] = useState('')
    const [apiKey, setApiKey] = useState('')
    const [apiKeyDirty, setApiKeyDirty] = useState(false)
    const [basicAuthUsername, setBasicAuthUsername] = useState('')
    const [basicAuthPassword, setBasicAuthPassword] = useState('')
    const [basicAuthPasswordDirty, setBasicAuthPasswordDirty] = useState(false)

    const activeProviderInfo = useMemo(
        () => availableProviders.find((p) => p.id === activeProviderId) || null,
        [availableProviders, activeProviderId]
    )

    const providerOptions = useMemo<SelectOption[]>(
        () => [
            { value: '', label: t('common.notSet') },
            ...availableProviders.map((p) => ({ value: p.id, label: p.name || p.id })),
        ],
        [availableProviders, t]
    )
    const storedProviderConfig = useMemo(() => providers[activeProviderId] || {}, [providers, activeProviderId])
    const hasStoredApiHost = (storedProviderConfig.api_host || '').trim() !== ''
    const hasStoredApiKey = (storedProviderConfig.api_key || '').trim() !== ''
    const hasStoredBasicAuthUsername = (storedProviderConfig.basic_auth_username || '').trim() !== ''
    const hasStoredBasicAuthPassword = (storedProviderConfig.basic_auth_password || '').trim() !== ''
    const supportsFetchResults = activeProviderInfo?.supports_max_results ?? true
    const supportsTimeFilter = activeProviderInfo?.supports_time_filter ?? false
    const showSearchEngineField = activeProviderId === 'zhipu'
    const showApiHostField =
        activeProviderId.trim() !== '' && (!!activeProviderInfo?.requires_api_host || hasStoredApiHost)
    const showApiKeyField =
        activeProviderId.trim() !== '' && (!!activeProviderInfo?.requires_api_key || hasStoredApiKey)
    const showBasicAuthUsernameField =
        activeProviderId.trim() !== '' && (!!activeProviderInfo?.supports_basic_auth || hasStoredBasicAuthUsername)
    const showBasicAuthPasswordField =
        activeProviderId.trim() !== '' && (!!activeProviderInfo?.supports_basic_auth || hasStoredBasicAuthPassword)

    const showMessageToast = (msg: string, type: 'success' | 'error' = 'success') => showToast(msg, type)

    const syncActiveProviderFields = (id: string, allProviders: Record<string, CornerstoneWebSearchProviderConfig>) => {
        const cfg = allProviders[id] || {}
        setApiHost(cfg.api_host || '')
        setSearchEngine(id === 'zhipu' ? cfg.search_engine || 'search_std' : 'search_std')
        setBasicAuthUsername(cfg.basic_auth_username || '')
        setApiKey('')
        setApiKeyDirty(false)
        setBasicAuthPassword('')
        setBasicAuthPasswordDirty(false)
    }

    const loadData = async () => {
        setLoading(true)
        try {
            const settings = await cornerstoneWebSearchService.getSettings()
            setAvailableProviders(settings.available_providers || [])
            setProviders(settings.providers || {})
            setActiveProviderId(settings.active_provider_id || '')
            setMaxResults(settings.max_results || 5)
            setFetchResults(settings.fetch_results || settings.max_results || 5)
            setExcludeDomainsText((settings.exclude_domains || []).join('\n'))
            setSearchWithTime(!!settings.search_with_time)
            setTimeoutSeconds(settings.timeout_seconds || 20)
            syncActiveProviderFields(settings.active_provider_id || '', settings.providers || {})
        } catch (error) {
            const msg = error instanceof Error ? error.message : t('settings.settingFailed')
            showMessageToast(msg, 'error')
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        loadData()
    }, [])

    useEffect(() => {
        setFetchResults((current) => Math.max(current, maxResults))
    }, [maxResults])

    const handleSave = async () => {
        if (saving) return
        setSaving(true)
        try {
            const providersPatch: Record<string, CornerstoneWebSearchProviderConfig> = {}
            if (activeProviderId.trim() !== '') {
                const providerPatch: CornerstoneWebSearchProviderConfig = {}
                if (showApiHostField) {
                    providerPatch.api_host = apiHost.trim()
                }
                if (showSearchEngineField) {
                    providerPatch.search_engine = searchEngine || 'search_std'
                }
                if (showBasicAuthUsernameField) {
                    providerPatch.basic_auth_username = basicAuthUsername.trim()
                }
                if (showApiKeyField && apiKeyDirty) {
                    providerPatch.api_key = apiKey.trim()
                }
                if (showBasicAuthPasswordField && basicAuthPasswordDirty) {
                    providerPatch.basic_auth_password = basicAuthPassword.trim()
                }
                if (Object.keys(providerPatch).length > 0) {
                    providersPatch[activeProviderId] = providerPatch
                }
            }

            const settingsPatch: Partial<CornerstoneWebSearchSettings> = {
                active_provider_id: activeProviderId,
                max_results: maxResults,
                exclude_domains: splitExcludeDomains(excludeDomainsText),
                timeout_seconds: timeoutSeconds,
                ...(supportsFetchResults ? { fetch_results: Math.max(fetchResults, maxResults) } : {}),
                ...(supportsTimeFilter ? { search_with_time: searchWithTime } : {}),
                ...(Object.keys(providersPatch).length > 0 ? { providers: providersPatch } : {}),
            }

            const updated = await cornerstoneWebSearchService.updateSettings(settingsPatch)

            setAvailableProviders(updated.available_providers || [])
            setProviders(updated.providers || {})
            setFetchResults(updated.fetch_results || updated.max_results || 5)
            showMessageToast(t('settings.webSearchSaved'))
            syncActiveProviderFields(updated.active_provider_id || '', updated.providers || {})
        } catch (error) {
            const msg = error instanceof Error ? error.message : t('settings.settingFailed')
            showMessageToast(msg, 'error')
        } finally {
            setSaving(false)
        }
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
                <button className="back-button" onClick={onBack} aria-label={t('common.back')}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('settings.webSearch')}</div>
                {!loading && (
                    <button
                        type="button"
                        className="ui-btn ui-btn--primary ui-btn--sm"
                        onClick={() => void handleSave()}
                        disabled={saving}
                    >
                        {saving ? t('common.saving') : t('common.save')}
                    </button>
                )}
            </div>

            {loading ? (
                <div className="provider-settings-loading" role="status">
                    <span className="ui-spinner" aria-hidden="true" />
                    {t('common.loading')}
                </div>
            ) : (
                <div className="provider-settings-content">
                    <Group title={t('settingsManagement.webSearch.service')}>
                        <SettingsField
                            label={t('settings.webSearchProvider')}
                            hint={
                                activeProviderInfo ? (
                                    <span className="settings-mono">{activeProviderInfo.id}</span>
                                ) : null
                            }
                        >
                            <CustomSelect
                                value={activeProviderId}
                                options={providerOptions}
                                ariaLabel={t('settings.webSearchProvider')}
                                disabled={saving}
                                onChange={(next) => {
                                    setActiveProviderId(next)
                                    syncActiveProviderFields(next, providers)
                                }}
                            />
                        </SettingsField>

                        {activeProviderId.trim() !== '' && showApiHostField && (
                            <SettingsField label={t('settings.webSearchApiHost')}>
                                <input
                                    className="settings-input"
                                    aria-label={t('settings.webSearchApiHost')}
                                    value={apiHost}
                                    onChange={(e) => setApiHost(e.target.value)}
                                    placeholder="https://..."
                                    disabled={saving}
                                />
                            </SettingsField>
                        )}

                        {activeProviderId.trim() !== '' && showSearchEngineField && (
                            <SettingsField label={t('settings.webSearchSearchEngine')}>
                                <CustomSelect
                                    value={searchEngine}
                                    options={ZHIPU_SEARCH_ENGINE_OPTIONS}
                                    ariaLabel={t('settings.webSearchSearchEngine')}
                                    disabled={saving}
                                    onChange={setSearchEngine}
                                />
                            </SettingsField>
                        )}

                        {activeProviderId.trim() !== '' && showApiKeyField && (
                            <SettingsField
                                label={t('settings.webSearchApiKey')}
                                hint={t('settings.webSearchApiKeyHint')}
                            >
                                <input
                                    className="settings-input"
                                    aria-label={t('settings.webSearchApiKey')}
                                    value={apiKey}
                                    onChange={(e) => {
                                        setApiKey(e.target.value)
                                        setApiKeyDirty(true)
                                    }}
                                    placeholder={hasStoredApiKey ? '****' : ''}
                                    disabled={saving}
                                />
                            </SettingsField>
                        )}

                        {activeProviderId.trim() !== '' && showBasicAuthUsernameField && (
                            <SettingsField label={t('settings.webSearchBasicAuthUsername')}>
                                <input
                                    className="settings-input"
                                    aria-label={t('settings.webSearchBasicAuthUsername')}
                                    value={basicAuthUsername}
                                    onChange={(e) => setBasicAuthUsername(e.target.value)}
                                    placeholder=""
                                    disabled={saving}
                                />
                            </SettingsField>
                        )}

                        {activeProviderId.trim() !== '' && showBasicAuthPasswordField && (
                            <SettingsField
                                label={t('settings.webSearchBasicAuthPassword')}
                                hint={t('settings.webSearchBasicAuthHint')}
                            >
                                <input
                                    className="settings-input"
                                    aria-label={t('settings.webSearchBasicAuthPassword')}
                                    value={basicAuthPassword}
                                    onChange={(e) => {
                                        setBasicAuthPassword(e.target.value)
                                        setBasicAuthPasswordDirty(true)
                                    }}
                                    placeholder={hasStoredBasicAuthPassword ? '****' : ''}
                                    disabled={saving}
                                />
                            </SettingsField>
                        )}
                    </Group>

                    <Group title={t('settingsManagement.webSearch.results')}>
                        <SettingsField label={t('settings.webSearchMaxResults')}>
                            <NumericInput
                                className="settings-input"
                                aria-label={t('settings.webSearchMaxResults')}
                                value={maxResults}
                                onValueChange={setMaxResults}
                                parseAs="int"
                                min={1}
                                max={50}
                                disabled={saving}
                            />
                        </SettingsField>

                        {supportsFetchResults && (
                            <SettingsField label={t('settings.webSearchFetchResults')}>
                                <NumericInput
                                    className="settings-input"
                                    aria-label={t('settings.webSearchFetchResults')}
                                    value={fetchResults}
                                    onValueChange={setFetchResults}
                                    parseAs="int"
                                    min={maxResults}
                                    max={50}
                                    disabled={saving}
                                />
                            </SettingsField>
                        )}

                        <SettingsField label={t('settings.webSearchTimeoutSeconds')}>
                            <NumericInput
                                className="settings-input"
                                aria-label={t('settings.webSearchTimeoutSeconds')}
                                value={timeoutSeconds}
                                onValueChange={setTimeoutSeconds}
                                parseAs="int"
                                min={1}
                                max={120}
                                disabled={saving}
                            />
                        </SettingsField>

                        {supportsTimeFilter && (
                            <SettingsField label={t('settings.webSearchSearchWithTime')}>
                                <Toggle
                                    label={t('settings.webSearchSearchWithTime')}
                                    checked={searchWithTime}
                                    onChange={setSearchWithTime}
                                    disabled={saving}
                                />
                            </SettingsField>
                        )}
                    </Group>

                    <Group title={t('settingsManagement.webSearch.excludeDomains')}>
                        <SettingsField label={t('settings.webSearchExcludeDomains')} stacked>
                            <textarea
                                className="settings-textarea"
                                aria-label={t('settings.webSearchExcludeDomains')}
                                value={excludeDomainsText}
                                onChange={(e) => setExcludeDomainsText(e.target.value)}
                                placeholder={t('settings.webSearchExcludeDomainsHint')}
                                disabled={saving}
                                rows={4}
                            />
                        </SettingsField>
                    </Group>
                </div>
            )}

            <AnimatePresence>
                {saving && (
                    <ModalOverlay
                        aria-label={t('common.saving')}
                        onDismiss={() => {
                            // Saving is the only open state; keep dismissal blocked until it finishes.
                        }}
                        className="prompt-modal-overlay"
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                    />
                )}
            </AnimatePresence>
        </motion.div>
    )
}

export default CornerstoneWebSearchSettingsPanel
