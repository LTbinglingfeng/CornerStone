import { useEffect, useMemo, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import {
    webSearchService,
    type WebSearchProviderConfig,
    type WebSearchProviderInfo,
    type WebSearchSettings,
} from '../services/webSearchService'
import { useT } from '../contexts/I18nContext'
import { NumericInput } from './NumericInput'
import { CustomSelect, type SelectOption } from './provider'
import { centerModalVariants, drawerVariants, overlayVariants } from '../utils/motion'
import './ProviderSettings.css'

interface WebSearchSettingsProps {
    onBack: () => void
}

const splitExcludeDomains = (raw: string): string[] =>
    raw
        .split(/[\n,]/g)
        .map((part) => part.trim())
        .filter((part) => part !== '')

const WebSearchSettingsPanel: React.FC<WebSearchSettingsProps> = ({ onBack }) => {
    const { t } = useT()

    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [message, setMessage] = useState('')
    const [messageType, setMessageType] = useState<'success' | 'error'>('success')

    const [availableProviders, setAvailableProviders] = useState<WebSearchProviderInfo[]>([])
    const [providers, setProviders] = useState<Record<string, WebSearchProviderConfig>>({})

    const [activeProviderId, setActiveProviderId] = useState('')
    const [maxResults, setMaxResults] = useState(5)
    const [fetchResults, setFetchResults] = useState(5)
    const [excludeDomainsText, setExcludeDomainsText] = useState('')
    const [searchWithTime, setSearchWithTime] = useState(false)
    const [timeoutSeconds, setTimeoutSeconds] = useState(20)

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
    const hasStoredApiKey = (storedProviderConfig.api_key || '').trim() !== ''
    const hasStoredBasicAuthPassword = (storedProviderConfig.basic_auth_password || '').trim() !== ''

    const showMessageToast = (msg: string, type: 'success' | 'error' = 'success') => {
        setMessage(msg)
        setMessageType(type)
        setTimeout(() => {
            setMessage('')
            setMessageType('success')
        }, 2000)
    }

    const syncActiveProviderFields = (id: string, allProviders: Record<string, WebSearchProviderConfig>) => {
        const cfg = allProviders[id] || {}
        setApiHost(cfg.api_host || '')
        setBasicAuthUsername(cfg.basic_auth_username || '')
        setApiKey('')
        setApiKeyDirty(false)
        setBasicAuthPassword('')
        setBasicAuthPasswordDirty(false)
    }

    const loadData = async () => {
        setLoading(true)
        try {
            const settings = await webSearchService.getSettings()
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
            const providersPatch: Record<string, WebSearchProviderConfig> = {}
            const nextFetchResults = Math.max(fetchResults, maxResults)
            if (activeProviderId.trim() !== '') {
                providersPatch[activeProviderId] = {
                    api_host: apiHost,
                    basic_auth_username: basicAuthUsername,
                    ...(apiKeyDirty ? { api_key: apiKey.trim() } : {}),
                    ...(basicAuthPasswordDirty ? { basic_auth_password: basicAuthPassword.trim() } : {}),
                }
            }

            const updated = await webSearchService.updateSettings({
                active_provider_id: activeProviderId,
                max_results: maxResults,
                fetch_results: nextFetchResults,
                exclude_domains: splitExcludeDomains(excludeDomainsText),
                search_with_time: searchWithTime,
                timeout_seconds: timeoutSeconds,
                ...(Object.keys(providersPatch).length > 0 ? { providers: providersPatch } : {}),
            } as Partial<WebSearchSettings>)

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
                <button className="back-button" onClick={onBack}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('settings.webSearch')}</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : (
                <div className="provider-settings-content">
                    <div className="settings-group">
                        <label className="settings-label">{t('settings.webSearchProvider')}</label>
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
                        {activeProviderInfo && (
                            <p className="prompt-modal-hint memory-toggle-hint">{activeProviderInfo.id}</p>
                        )}
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('settings.webSearchMaxResults')}</label>
                        <NumericInput
                            className="settings-input"
                            value={maxResults}
                            onValueChange={setMaxResults}
                            parseAs="int"
                            min={1}
                            max={50}
                            disabled={saving}
                        />
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('settings.webSearchFetchResults')}</label>
                        <NumericInput
                            className="settings-input"
                            value={fetchResults}
                            onValueChange={setFetchResults}
                            parseAs="int"
                            min={maxResults}
                            max={50}
                            disabled={saving}
                        />
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('settings.webSearchTimeoutSeconds')}</label>
                        <NumericInput
                            className="settings-input"
                            value={timeoutSeconds}
                            onValueChange={setTimeoutSeconds}
                            parseAs="int"
                            min={1}
                            max={120}
                            disabled={saving}
                        />
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('settings.webSearchExcludeDomains')}</label>
                        <textarea
                            className="settings-textarea"
                            value={excludeDomainsText}
                            onChange={(e) => setExcludeDomainsText(e.target.value)}
                            placeholder={t('settings.webSearchExcludeDomainsHint')}
                            disabled={saving}
                            rows={4}
                        />
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('settings.webSearchSearchWithTime')}</label>
                        <div className="modal-toggle-wrapper">
                            <label className="toggle-switch">
                                <input
                                    type="checkbox"
                                    checked={searchWithTime}
                                    onChange={(e) => setSearchWithTime(e.target.checked)}
                                    disabled={saving}
                                />
                                <span className="toggle-slider"></span>
                            </label>
                            <span className="toggle-label">
                                {searchWithTime ? t('common.enable') : t('common.disable')}
                            </span>
                        </div>
                    </div>

                    {activeProviderId.trim() !== '' && (
                        <>
                            <div className="settings-group">
                                <label className="settings-label">{t('settings.webSearchApiHost')}</label>
                                <input
                                    className="settings-input"
                                    value={apiHost}
                                    onChange={(e) => setApiHost(e.target.value)}
                                    placeholder="https://..."
                                    disabled={saving}
                                />
                            </div>

                            <div className="settings-group">
                                <label className="settings-label">{t('settings.webSearchApiKey')}</label>
                                <input
                                    className="settings-input"
                                    value={apiKey}
                                    onChange={(e) => {
                                        setApiKey(e.target.value)
                                        setApiKeyDirty(true)
                                    }}
                                    placeholder={hasStoredApiKey ? '****' : ''}
                                    disabled={saving}
                                />
                                <p className="prompt-modal-hint memory-toggle-hint">
                                    {t('settings.webSearchApiKeyHint')}
                                </p>
                            </div>

                            <div className="settings-group">
                                <label className="settings-label">{t('settings.webSearchBasicAuthUsername')}</label>
                                <input
                                    className="settings-input"
                                    value={basicAuthUsername}
                                    onChange={(e) => setBasicAuthUsername(e.target.value)}
                                    placeholder=""
                                    disabled={saving}
                                />
                            </div>

                            <div className="settings-group">
                                <label className="settings-label">{t('settings.webSearchBasicAuthPassword')}</label>
                                <input
                                    className="settings-input"
                                    value={basicAuthPassword}
                                    onChange={(e) => {
                                        setBasicAuthPassword(e.target.value)
                                        setBasicAuthPasswordDirty(true)
                                    }}
                                    placeholder={hasStoredBasicAuthPassword ? '****' : ''}
                                    disabled={saving}
                                />
                                <p className="prompt-modal-hint memory-toggle-hint">
                                    {t('settings.webSearchBasicAuthHint')}
                                </p>
                            </div>
                        </>
                    )}

                    <button className="settings-save-btn" onClick={handleSave} disabled={saving}>
                        {saving ? t('common.saving') : t('common.save')}
                    </button>

                    <AnimatePresence>
                        {message && (
                            <motion.div
                                className={`settings-message ${messageType}`}
                                initial="hidden"
                                animate="visible"
                                exit="hidden"
                                variants={centerModalVariants}
                            >
                                {message}
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>
            )}

            <AnimatePresence>
                {saving && (
                    <motion.div
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

export default WebSearchSettingsPanel
