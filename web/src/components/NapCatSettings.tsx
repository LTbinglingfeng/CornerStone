import { useEffect, useMemo, useState } from 'react'
import { motion } from 'motion/react'
import { getPrompts } from '../services/api'
import { napCatService, type NapCatSettings } from '../services/napcatService'
import { useT } from '../contexts/I18nContext'
import type { Prompt } from '../types/chat'
import { CustomSelect } from './provider'
import { useToast } from '../contexts/ToastContext'
import { drawerVariants } from '../utils/motion'
import './ProviderSettings.css'

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
    const { t } = useT()
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
            disabled: t('napCat.disabled'),
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
            className="provider-settings napcat-settings"
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
                <div className="provider-settings-title">{t('napCat.title')}</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : (
                <div className="provider-settings-content">
                    <div className="provider-card active" style={{ cursor: 'default' }}>
                        <div
                            style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}
                        >
                            <div style={{ color: 'var(--text-primary)', fontSize: 15, fontWeight: 600 }}>
                                {t('napCat.runStatus')}
                            </div>
                            <div style={{ color: 'var(--text-secondary)', fontSize: 13 }}>{statusLabel}</div>
                        </div>

                        <div
                            style={{
                                marginTop: 10,
                                display: 'flex',
                                flexWrap: 'wrap',
                                gap: 8,
                                color: 'var(--text-secondary)',
                                fontSize: 12,
                            }}
                        >
                            <div>
                                {t('napCat.selfId')}：{settings?.self_id || '-'}
                            </div>
                            <div>
                                {t('napCat.nickname')}：{settings?.nickname || '-'}
                            </div>
                        </div>

                        {settings?.last_error && (
                            <div
                                style={{
                                    marginTop: 12,
                                    color: '#c65746',
                                    fontSize: 12,
                                    lineHeight: 1.5,
                                    wordBreak: 'break-word',
                                }}
                            >
                                {settings.last_error}
                            </div>
                        )}
                    </div>

                    <div className="settings-group" style={{ marginTop: 16 }}>
                        <label className="settings-label">{t('napCat.wsUrl')}</label>
                        <div
                            style={{
                                padding: '12px 14px',
                                borderRadius: 10,
                                border: '1px solid var(--border-color)',
                                background: 'var(--bg-tertiary)',
                                color: 'var(--text-primary)',
                                fontSize: 13,
                                wordBreak: 'break-all',
                            }}
                        >
                            {wsUrlDisplay}
                        </div>
                        <p className="prompt-modal-hint" style={{ marginTop: 8 }}>
                            {t('napCat.wsUrlHint')}
                        </p>
                        <p className="prompt-modal-hint" style={{ marginTop: 8 }}>
                            {t('napCat.messagePostFormatHint')}
                        </p>
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('napCat.enableChannel')}</label>
                        <div className="modal-toggle-wrapper">
                            <label className="toggle-switch">
                                <input
                                    type="checkbox"
                                    checked={form.enabled}
                                    onChange={(event) =>
                                        setForm((current) => ({
                                            ...current,
                                            enabled: event.target.checked,
                                        }))
                                    }
                                    disabled={saving}
                                />
                                <span className="toggle-slider"></span>
                            </label>
                            <span className="toggle-label">
                                {form.enabled ? t('common.enabled') : t('common.disabled')}
                            </span>
                        </div>
                        <p className="prompt-modal-hint memory-toggle-hint">{t('napCat.m1PrivateOnly')}</p>
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('napCat.bindPersona')}</label>
                        <CustomSelect
                            value={form.prompt_id}
                            options={promptOptions}
                            onChange={(value) => setForm((current) => ({ ...current, prompt_id: value }))}
                            ariaLabel={t('napCat.bindPersona')}
                            disabled={saving}
                        />
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('napCat.accessToken')}</label>
                        <input
                            className="settings-input"
                            value={form.access_token}
                            onChange={(event) =>
                                setForm((current) => ({ ...current, access_token: event.target.value }))
                            }
                            placeholder={
                                settings?.has_access_token ? t('napCat.tokenKeepHint') : t('napCat.tokenPlaceholder')
                            }
                            disabled={saving || form.clear_access_token}
                            type="password"
                        />
                        <div
                            style={{
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'space-between',
                                gap: 10,
                                marginTop: 10,
                            }}
                        >
                            <span style={{ color: 'var(--text-secondary)', fontSize: 12 }}>
                                {settings?.has_access_token ? t('napCat.savedToken') : t('napCat.notSavedToken')}
                            </span>
                            {settings?.has_access_token && (
                                <button
                                    type="button"
                                    className="add-button"
                                    style={{
                                        background: form.clear_access_token
                                            ? 'rgba(198, 87, 70, 0.9)'
                                            : 'var(--bg-secondary)',
                                        border: form.clear_access_token
                                            ? '1px solid rgba(198, 87, 70, 0.8)'
                                            : '1px solid var(--border-color)',
                                        color: form.clear_access_token ? '#fff' : 'var(--text-secondary)',
                                    }}
                                    onClick={() =>
                                        setForm((current) => ({
                                            ...current,
                                            clear_access_token: !current.clear_access_token,
                                            access_token: '',
                                        }))
                                    }
                                    disabled={saving}
                                >
                                    {form.clear_access_token ? t('napCat.markedCleared') : t('napCat.clearToken')}
                                </button>
                            )}
                        </div>
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('napCat.allowPrivate')}</label>
                        <div className="modal-toggle-wrapper">
                            <label className="toggle-switch">
                                <input
                                    type="checkbox"
                                    checked={form.allow_private}
                                    onChange={(event) =>
                                        setForm((current) => ({ ...current, allow_private: event.target.checked }))
                                    }
                                    disabled={saving}
                                />
                                <span className="toggle-slider"></span>
                            </label>
                            <span className="toggle-label">
                                {form.allow_private ? t('common.enabled') : t('common.disabled')}
                            </span>
                        </div>
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('napCat.sourceFilterMode')}</label>
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
                        <p className="prompt-modal-hint" style={{ marginTop: 8 }}>
                            {t('napCat.sourceFilterHint')}
                        </p>
                    </div>

                    <div className="settings-group">
                        <label className="settings-label">{t('napCat.allowedPrivateUserIds')}</label>
                        <textarea
                            className="settings-textarea"
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

                    <button type="button" className="save-button full-width" onClick={handleSave} disabled={saving}>
                        {saving ? t('common.saving') : t('napCat.saveSettings')}
                    </button>
                </div>
            )}
        </motion.div>
    )
}

export default NapCatSettingsPanel
