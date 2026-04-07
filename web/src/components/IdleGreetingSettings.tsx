import { useEffect, useMemo, useState } from 'react'
import { motion } from 'motion/react'
import { updateConfig } from '../services/api'
import type { IdleGreetingConfig, IdleGreetingTimeWindow } from '../types/chat'
import { useT } from '../contexts/I18nContext'
import { drawerVariants } from '../utils/motion'
import { NumericInput } from './NumericInput'
import './ProviderSettings.css'
import './Settings.css'

interface IdleGreetingSettingsProps {
    config: IdleGreetingConfig
    onBack: () => void
    onSaved: (config: IdleGreetingConfig) => void
}

const DEFAULT_IDLE_GREETING_CONFIG: IdleGreetingConfig = {
    enabled: false,
    time_windows: [{ start: '09:00', end: '22:00' }],
    idle_min_minutes: 100,
    idle_max_minutes: 120,
}

const EMPTY_WINDOW: IdleGreetingTimeWindow = {
    start: '09:00',
    end: '22:00',
}

const normalizeWindow = (window: IdleGreetingTimeWindow): IdleGreetingTimeWindow => ({
    start: (window.start || '').trim(),
    end: (window.end || '').trim(),
})

const normalizeIdleGreetingConfig = (config?: IdleGreetingConfig | null): IdleGreetingConfig => {
    if (!config) return DEFAULT_IDLE_GREETING_CONFIG
    return {
        enabled: !!config.enabled,
        time_windows:
            Array.isArray(config.time_windows) && config.time_windows.length > 0
                ? config.time_windows.map(normalizeWindow)
                : DEFAULT_IDLE_GREETING_CONFIG.time_windows,
        idle_min_minutes:
            Number.isFinite(config.idle_min_minutes) && config.idle_min_minutes > 0
                ? Math.round(config.idle_min_minutes)
                : DEFAULT_IDLE_GREETING_CONFIG.idle_min_minutes,
        idle_max_minutes:
            Number.isFinite(config.idle_max_minutes) && config.idle_max_minutes > 0
                ? Math.round(config.idle_max_minutes)
                : DEFAULT_IDLE_GREETING_CONFIG.idle_max_minutes,
    }
}

const isValidClockTime = (value: string) => {
    const trimmed = value.trim()
    if (!/^\d{2}:\d{2}$/.test(trimmed)) return false
    const [hourText, minuteText] = trimmed.split(':')
    const hour = Number.parseInt(hourText, 10)
    const minute = Number.parseInt(minuteText, 10)
    return hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59
}

const IdleGreetingSettingsPanel: React.FC<IdleGreetingSettingsProps> = ({ config, onBack, onSaved }) => {
    const { t } = useT()

    const [saving, setSaving] = useState(false)
    const [message, setMessage] = useState('')
    const [messageType, setMessageType] = useState<'success' | 'error'>('success')
    const [draft, setDraft] = useState<IdleGreetingConfig>(() => normalizeIdleGreetingConfig(config))

    useEffect(() => {
        setDraft(normalizeIdleGreetingConfig(config))
    }, [config])

    const showMessageToast = (nextMessage: string, type: 'success' | 'error' = 'success') => {
        setMessage(nextMessage)
        setMessageType(type)
        window.setTimeout(() => {
            setMessage('')
            setMessageType('success')
        }, 2000)
    }

    const validationError = useMemo(() => {
        if (!Number.isFinite(draft.idle_min_minutes) || draft.idle_min_minutes <= 0) {
            return t('settings.idleGreetingMinutesInvalid')
        }
        if (!Number.isFinite(draft.idle_max_minutes) || draft.idle_max_minutes <= 0) {
            return t('settings.idleGreetingMinutesInvalid')
        }
        if (draft.idle_min_minutes > draft.idle_max_minutes) {
            return t('settings.idleGreetingMinutesOrderError')
        }
        if (!Array.isArray(draft.time_windows) || draft.time_windows.length === 0) {
            return t('settings.idleGreetingNeedWindow')
        }
        for (const window of draft.time_windows) {
            if (!isValidClockTime(window.start) || !isValidClockTime(window.end) || window.start === window.end) {
                return t('settings.idleGreetingWindowInvalid')
            }
        }
        return ''
    }, [draft, t])

    const handleWindowChange = (index: number, patch: Partial<IdleGreetingTimeWindow>) => {
        setDraft((current) => ({
            ...current,
            time_windows: current.time_windows.map((window, windowIndex) =>
                windowIndex === index ? normalizeWindow({ ...window, ...patch }) : window
            ),
        }))
    }

    const handleAddWindow = () => {
        setDraft((current) => ({
            ...current,
            time_windows: [...current.time_windows, EMPTY_WINDOW],
        }))
    }

    const handleRemoveWindow = (index: number) => {
        setDraft((current) => ({
            ...current,
            time_windows: current.time_windows.filter((_, windowIndex) => windowIndex !== index),
        }))
    }

    const handleSave = async () => {
        if (saving) return
        if (validationError) {
            showMessageToast(validationError, 'error')
            return
        }

        const nextConfig = normalizeIdleGreetingConfig(draft)
        setSaving(true)
        try {
            const success = await updateConfig({ idle_greeting: nextConfig })
            if (!success) {
                showMessageToast(t('common.saveFailed'), 'error')
                return
            }
            onSaved(nextConfig)
            showMessageToast(t('settings.idleGreetingSaved'))
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
                <div className="provider-settings-title">{t('settings.idleGreeting')}</div>
                <div style={{ width: 44 }}></div>
            </div>

            <div className="provider-settings-content">
                <div className="settings-section">
                    <h3>{t('settings.idleGreeting')}</h3>
                    <p className="prompt-modal-hint">{t('settings.idleGreetingHint')}</p>

                    <div className="settings-group">
                        <label className="settings-label">{t('settings.idleGreetingEnabled')}</label>
                        <div className="modal-toggle-wrapper">
                            <label className="toggle-switch">
                                <input
                                    type="checkbox"
                                    checked={draft.enabled}
                                    onChange={(event) =>
                                        setDraft((current) => ({ ...current, enabled: event.target.checked }))
                                    }
                                    disabled={saving}
                                />
                                <span className="toggle-slider"></span>
                            </label>
                            <span className="toggle-label">
                                {draft.enabled ? t('common.enable') : t('common.disable')}
                            </span>
                        </div>
                    </div>
                </div>

                <div className="settings-section">
                    <h3>{t('settings.idleGreetingIdleRange')}</h3>
                    <div
                        style={{
                            display: 'grid',
                            gridTemplateColumns: '1fr 1fr',
                            gap: 12,
                        }}
                    >
                        <div className="settings-group" style={{ marginTop: 0 }}>
                            <label className="settings-label">{t('settings.idleGreetingMinMinutes')}</label>
                            <NumericInput
                                className="settings-input"
                                value={draft.idle_min_minutes}
                                onValueChange={(value) =>
                                    setDraft((current) => ({ ...current, idle_min_minutes: Math.round(value) }))
                                }
                                parseAs="int"
                                min={1}
                                max={10080}
                                disabled={saving}
                            />
                        </div>

                        <div className="settings-group" style={{ marginTop: 0 }}>
                            <label className="settings-label">{t('settings.idleGreetingMaxMinutes')}</label>
                            <NumericInput
                                className="settings-input"
                                value={draft.idle_max_minutes}
                                onValueChange={(value) =>
                                    setDraft((current) => ({ ...current, idle_max_minutes: Math.round(value) }))
                                }
                                parseAs="int"
                                min={1}
                                max={10080}
                                disabled={saving}
                            />
                        </div>
                    </div>
                    <p className="prompt-modal-hint">{t('settings.idleGreetingIdleRangeHint')}</p>
                </div>

                <div className="settings-section">
                    <h3>{t('settings.idleGreetingWindows')}</h3>
                    <p className="prompt-modal-hint">{t('settings.idleGreetingWindowsHint')}</p>

                    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                        {draft.time_windows.map((window, index) => (
                            <div
                                key={`${index}-${window.start}-${window.end}`}
                                style={{
                                    display: 'grid',
                                    gridTemplateColumns: '1fr 1fr auto',
                                    gap: 12,
                                    alignItems: 'end',
                                }}
                            >
                                <div className="settings-group" style={{ marginTop: 0 }}>
                                    <label className="settings-label">{t('settings.idleGreetingWindowStart')}</label>
                                    <input
                                        className="settings-input"
                                        value={window.start}
                                        onChange={(event) => handleWindowChange(index, { start: event.target.value })}
                                        placeholder="09:00"
                                        disabled={saving}
                                    />
                                </div>

                                <div className="settings-group" style={{ marginTop: 0 }}>
                                    <label className="settings-label">{t('settings.idleGreetingWindowEnd')}</label>
                                    <input
                                        className="settings-input"
                                        value={window.end}
                                        onChange={(event) => handleWindowChange(index, { end: event.target.value })}
                                        placeholder="22:00"
                                        disabled={saving}
                                    />
                                </div>

                                <button
                                    type="button"
                                    className="settings-save-btn"
                                    onClick={() => handleRemoveWindow(index)}
                                    disabled={saving || draft.time_windows.length <= 1}
                                    style={{ minWidth: 92, marginBottom: 0, padding: '0 16px' }}
                                >
                                    {t('common.delete')}
                                </button>
                            </div>
                        ))}
                    </div>

                    <button
                        type="button"
                        className="settings-entry-btn"
                        onClick={handleAddWindow}
                        style={{ marginTop: 12 }}
                        disabled={saving}
                    >
                        <div className="settings-entry-info">
                            <span className="settings-entry-label">{t('settings.idleGreetingAddWindow')}</span>
                            <span className="settings-entry-subvalue">{t('settings.idleGreetingWindowFormat')}</span>
                        </div>
                        <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                            <path d="M19 11H13V5h-2v6H5v2h6v6h2v-6h6z" />
                        </svg>
                    </button>
                </div>

                {validationError && <div className="settings-message error">{validationError}</div>}

                <button className="settings-save-btn" onClick={handleSave} disabled={saving}>
                    {saving ? t('common.saving') : t('common.save')}
                </button>

                {message && <div className={`settings-message ${messageType}`}>{message}</div>}
            </div>
        </motion.div>
    )
}

export default IdleGreetingSettingsPanel
