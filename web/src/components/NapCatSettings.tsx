import { useEffect, useMemo, useState } from 'react'
import { motion } from 'motion/react'
import { getPrompts } from '../services/api'
import { napCatService, type NapCatSettings } from '../services/napcatService'
import { useT } from '../contexts/I18nContext'
import type { Prompt } from '../types/chat'
import { CustomSelect } from './provider'
import { useToast } from '../contexts/ToastContext'
import { drawerVariants } from '../utils/motion'
import { getChannelsCopy } from '../i18n/channels'
import { Badge, Group, Icon, Row, Toggle } from './ui'
import { channelStatusTone } from './channelStatus'
import './ClawBotSettings.css'

interface NapCatSettingsProps {
    onBack: () => void
}

type NapCatFormState = {
    enabled: boolean
    access_token: string
    clear_access_token: boolean
    prompt_id: string
    allow_private: boolean
    source_filter_mode: 'all' | 'allowlist'
    allowed_private_user_ids_text: string
}

const buildAllowlistText = (items?: string[]) =>
    (items || [])
        .map((value) => value.trim())
        .filter(Boolean)
        .join('\n')

const parseAllowlistText = (text: string): string[] => {
    const seen = new Set<string>()
    const results: string[] = []
    text.split('\n')
        .map((line) => line.trim())
        .filter(Boolean)
        .forEach((line) => {
            if (seen.has(line)) return
            seen.add(line)
            results.push(line)
        })
    return results
}

const NapCatSettingsPanel: React.FC<NapCatSettingsProps> = ({ onBack }) => {
    const { t, locale } = useT()
    const copy = getChannelsCopy(locale).detail
    const { showToast } = useToast()
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [settings, setSettings] = useState<NapCatSettings | null>(null)
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [form, setForm] = useState<NapCatFormState>({
        enabled: false,
        access_token: '',
        clear_access_token: false,
        prompt_id: '',
        allow_private: true,
        source_filter_mode: 'all',
        allowed_private_user_ids_text: '',
    })

    useEffect(() => {
        void loadData()
    }, [])

    const loadData = async () => {
        setLoading(true)
        try {
            const [nextSettings, promptList] = await Promise.all([napCatService.getSettings(), getPrompts()])
            setSettings(nextSettings)
            setPrompts(promptList)
            setForm({
                enabled: nextSettings.enabled,
                access_token: '',
                clear_access_token: false,
                prompt_id: nextSettings.prompt_id || '',
                allow_private: nextSettings.allow_private ?? true,
                source_filter_mode: nextSettings.source_filter_mode === 'allowlist' ? 'allowlist' : 'all',
                allowed_private_user_ids_text: buildAllowlistText(nextSettings.allowed_private_user_ids),
            })
        } catch (error) {
            const message = error instanceof Error ? error.message : t('common.loadFailed')
            showToast(message, 'error')
        } finally {
            setLoading(false)
        }
    }

    const syncSettings = (nextSettings: NapCatSettings) => {
        setSettings(nextSettings)
        setForm((current) => ({
            ...current,
            enabled: nextSettings.enabled,
            access_token: '',
            clear_access_token: false,
            prompt_id: nextSettings.prompt_id || '',
            allow_private: nextSettings.allow_private ?? current.allow_private,
            source_filter_mode: nextSettings.source_filter_mode === 'allowlist' ? 'allowlist' : 'all',
            allowed_private_user_ids_text: buildAllowlistText(nextSettings.allowed_private_user_ids),
        }))
    }

    const promptOptions = useMemo(
        () => [
            { value: '', label: t('napCat.noPersonaBind') },
            ...prompts.map((prompt) => ({ value: prompt.id, label: prompt.name })),
        ],
        [prompts, t]
    )

    const statusLabel = useMemo(() => {
        const status = (settings?.status || '').trim()
        const statusMap: Record<string, string> = {
            disabled: t('common.disable'),
            missing_token: t('napCat.missingToken'),
            connected: t('napCat.connected'),
            disconnected: t('napCat.disconnected'),
            error: t('napCat.error'),
        }
        return statusMap[status] || status || t('common.notConfigured')
    }, [settings?.status, t])

    const wsUrlBase = useMemo(() => {
        const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
        return `${scheme}://${window.location.host}/api/channel/napcat/ws`
    }, [])

    const wsUrlDisplay = useMemo(() => {
        const token = form.access_token.trim()
        if (!token) return `${wsUrlBase}?access_token=<token>`
        return `${wsUrlBase}?access_token=${encodeURIComponent(token)}`
    }, [form.access_token, wsUrlBase])

    const handleSave = async () => {
        if (saving) return
        setSaving(true)
        try {
            const nextSettings = await napCatService.updateSettings({
                enabled: form.enabled,
                access_token: form.access_token.trim() || undefined,
                clear_access_token: form.clear_access_token,
                prompt_id: form.prompt_id || undefined,
                allow_private: form.allow_private,
                source_filter_mode: form.source_filter_mode,
                allowed_private_user_ids: parseAllowlistText(form.allowed_private_user_ids_text),
            })
            syncSettings(nextSettings)
            showToast(t('napCat.saved'), 'success')
        } catch (error) {
            const message = error instanceof Error ? error.message : t('common.saveFailed')
            showToast(message, 'error')
        } finally {
            setSaving(false)
        }
    }

    const allowlistDisabled = form.source_filter_mode !== 'allowlist'

    return (
        <motion.div
            className="provider-settings napcat-settings channel-detail"
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
                <div className="provider-settings-title">
                    <span className="provider-settings-title-inline">
                        <span>{t('napCat.title')}</span>
                        <span className="feature-beta-badge">BETA</span>
                    </span>
                </div>
                {settings && !loading && (
                    <div className="channel-detail-actions">
                        <button
                            type="button"
                            className="save-button ui-btn ui-btn--primary ui-btn--sm"
                            onClick={handleSave}
                            disabled={saving}
                        >
                            {saving ? t('common.saving') : t('napCat.saveSettings')}
                        </button>
                    </div>
                )}
            </div>

            {loading ? (
                <div className="provider-settings-loading">
                    <span className="ui-spinner" aria-hidden="true" />
                    {t('common.loading')}
                </div>
            ) : !settings ? (
                <div className="console-load-error" role="alert">
                    <p>{copy.loadFailed}</p>
                    <button className="ui-btn" onClick={() => void loadData()}>
                        {t('console.refresh')}
                    </button>
                </div>
            ) : (
                <div className="provider-settings-content">
                    <Group title={copy.status}>
                        <Row
                            label={t('napCat.runStatus')}
                            trailing={
                                <Badge tone={channelStatusTone(settings.status)} dot>
                                    {statusLabel}
                                </Badge>
                            }
                        />
                        <Row label={t('napCat.nickname')} value={settings.nickname || '—'} />
                        <Row label={t('napCat.selfId')} value={settings.self_id || '—'} />
                        {settings.last_error && (
                            <div className="channel-field channel-error" role="alert">
                                <span className="channel-error__label">{copy.lastError}</span>
                                <span className="channel-error__text">{settings.last_error}</span>
                            </div>
                        )}
                    </Group>

                    <Group title={copy.connection}>
                        <div className="channel-field">
                            <div className="ui-field">
                                <span className="ui-field__label">{t('napCat.wsUrl')}</span>
                                <div className="channel-code">
                                    <code>{wsUrlDisplay}</code>
                                    <button
                                        type="button"
                                        className="ui-icon-btn channel-code__copy"
                                        aria-label={copy.copy}
                                        title={copy.copy}
                                        onClick={() => {
                                            navigator.clipboard
                                                ?.writeText(wsUrlDisplay)
                                                .then(() => showToast(copy.copied, 'success'))
                                                .catch(() => showToast(copy.copyFailed, 'error'))
                                        }}
                                    >
                                        <Icon name="copy" />
                                    </button>
                                </div>
                                <span className="ui-field__hint">{t('napCat.wsUrlHint')}</span>
                                <span className="ui-field__hint">{t('napCat.messagePostFormatHint')}</span>
                            </div>
                        </div>
                        <div className="channel-field">
                            <div className="ui-field">
                                <label className="ui-field__label" htmlFor="napcat-access-token">
                                    {t('napCat.accessToken')}
                                </label>
                                <input
                                    id="napcat-access-token"
                                    className="ui-input"
                                    aria-label={t('napCat.accessToken')}
                                    value={form.access_token}
                                    onChange={(event) =>
                                        setForm((current) => ({ ...current, access_token: event.target.value }))
                                    }
                                    placeholder={
                                        settings.has_access_token
                                            ? t('napCat.tokenKeepHint')
                                            : t('napCat.tokenPlaceholder')
                                    }
                                    disabled={saving || form.clear_access_token}
                                    type="password"
                                />
                                <div className="channel-token-row">
                                    <span className="ui-field__hint">
                                        {settings.has_access_token ? t('napCat.savedToken') : t('napCat.notSavedToken')}
                                    </span>
                                    {settings.has_access_token && (
                                        <button
                                            type="button"
                                            className={`ui-btn ui-btn--sm ${form.clear_access_token ? 'ui-btn--danger-solid' : 'ui-btn--ghost'}`}
                                            aria-pressed={form.clear_access_token}
                                            onClick={() =>
                                                setForm((current) => ({
                                                    ...current,
                                                    clear_access_token: !current.clear_access_token,
                                                    access_token: '',
                                                }))
                                            }
                                            disabled={saving}
                                        >
                                            {form.clear_access_token
                                                ? t('napCat.markedCleared')
                                                : t('napCat.clearToken')}
                                        </button>
                                    )}
                                </div>
                            </div>
                        </div>
                    </Group>

                    <Group title={copy.basics} footer={t('napCat.m1PrivateOnly')} className="channel-group--overflow">
                        <Row
                            label={t('napCat.enableChannel')}
                            trailing={
                                <Toggle
                                    label={t('napCat.enableChannel')}
                                    checked={form.enabled}
                                    onChange={(enabled) => setForm((current) => ({ ...current, enabled }))}
                                    disabled={saving}
                                />
                            }
                        />
                        <div className="channel-field">
                            <div className="ui-field">
                                <span className="ui-field__label">{t('napCat.bindPersona')}</span>
                                <CustomSelect
                                    value={form.prompt_id}
                                    options={promptOptions}
                                    onChange={(value) => setForm((current) => ({ ...current, prompt_id: value }))}
                                    ariaLabel={t('napCat.bindPersona')}
                                    disabled={saving}
                                />
                            </div>
                        </div>
                    </Group>

                    <Group title={copy.access} className="channel-group--overflow">
                        <Row
                            label={t('napCat.allowPrivate')}
                            trailing={
                                <Toggle
                                    label={t('napCat.allowPrivate')}
                                    checked={form.allow_private}
                                    onChange={(allow_private) => setForm((current) => ({ ...current, allow_private }))}
                                    disabled={saving}
                                />
                            }
                        />
                        <div className="channel-field">
                            <div className="ui-field">
                                <span className="ui-field__label">{t('napCat.sourceFilterMode')}</span>
                                <CustomSelect
                                    value={form.source_filter_mode}
                                    options={[
                                        { value: 'all', label: t('napCat.filterAll') },
                                        { value: 'allowlist', label: t('napCat.filterAllowlist') },
                                    ]}
                                    onChange={(value) =>
                                        setForm((current) => ({
                                            ...current,
                                            source_filter_mode: value === 'allowlist' ? 'allowlist' : 'all',
                                        }))
                                    }
                                    ariaLabel={t('napCat.sourceFilterMode')}
                                    disabled={saving}
                                />
                                <span className="ui-field__hint">{t('napCat.sourceFilterHint')}</span>
                            </div>
                        </div>
                        <div className="channel-field">
                            <div className="ui-field">
                                <label className="ui-field__label" htmlFor="napcat-allowlist">
                                    {t('napCat.allowedPrivateUserIds')}
                                </label>
                                <textarea
                                    id="napcat-allowlist"
                                    className="ui-textarea channel-allowlist"
                                    aria-label={t('napCat.allowedPrivateUserIds')}
                                    value={form.allowed_private_user_ids_text}
                                    onChange={(event) =>
                                        setForm((current) => ({
                                            ...current,
                                            allowed_private_user_ids_text: event.target.value,
                                        }))
                                    }
                                    placeholder={t('napCat.allowlistPlaceholder')}
                                    disabled={saving || allowlistDisabled}
                                />
                            </div>
                        </div>
                    </Group>
                </div>
            )}
        </motion.div>
    )
}

export default NapCatSettingsPanel
