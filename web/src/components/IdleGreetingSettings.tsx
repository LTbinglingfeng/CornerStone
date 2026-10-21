import { useEffect, useMemo, useState } from 'react'
import { motion } from 'motion/react'
import { updateConfig } from '../services/api'
import type { IdleGreetingConfig, IdleGreetingTimeWindow } from '../types/chat'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { Group, Icon, Row, Toggle } from './ui'
import { SettingsField } from './SettingsField'
import { drawerVariants } from '../utils/motion'
import { NumericInput } from './NumericInput'
import './ProviderSettings.css'

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
    const { showToast } = useToast()

    const [saving, setSaving] = useState(false)
    const [draft, setDraft] = useState<IdleGreetingConfig>(() => normalizeIdleGreetingConfig(config))
    const [showValidationErrors, setShowValidationErrors] = useState(false)

    useEffect(() => {
        setDraft(normalizeIdleGreetingConfig(config))
        setShowValidationErrors(false)
    }, [config])

    const showMessageToast = (nextMessage: string, type: 'success' | 'error' = 'success') =>
        showToast(nextMessage, type)

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
            time_windows: [...current.time_windows, { ...EMPTY_WINDOW }],
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
        setShowValidationErrors(true)
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
                <button className="back-button" onClick={onBack} aria-label={t('common.back')}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('settings.idleGreeting')}</div>
                <button
                    type="button"
                    className="ui-btn ui-btn--primary ui-btn--sm"
                    onClick={() => void handleSave()}
                    disabled={saving}
                >
                    {saving ? t('common.saving') : t('common.save')}
                </button>
            </div>

            <div className="provider-settings-content">
                <Group footer={t('settings.idleGreetingHint')}>
                    <Row
                        icon="smile"
                        label={t('settingsManagement.idleGreeting.enable')}
                        trailing={
                            <Toggle
                                label={t('settingsManagement.idleGreeting.enable')}
                                checked={draft.enabled}
                                disabled={saving}
                                onChange={(checked) => setDraft((current) => ({ ...current, enabled: checked }))}
                            />
                        }
                    />
                </Group>

                <Group title={t('settings.idleGreetingIdleRange')} footer={t('settings.idleGreetingIdleRangeHint')}>
                    <SettingsField label={t('settings.idleGreetingMinMinutes')}>
                        <NumericInput
                            className="settings-input"
                            aria-label={t('settings.idleGreetingMinMinutes')}
                            value={draft.idle_min_minutes}
                            onValueChange={(value) =>
                                setDraft((current) => ({ ...current, idle_min_minutes: Math.round(value) }))
                            }
                            parseAs="int"
                            min={1}
                            max={10080}
                            disabled={saving}
                        />
                        <span className="settings-field__suffix">{t('settingsManagement.idleGreeting.minutes')}</span>
                    </SettingsField>
                    <SettingsField label={t('settings.idleGreetingMaxMinutes')}>
                        <NumericInput
                            className="settings-input"
                            aria-label={t('settings.idleGreetingMaxMinutes')}
                            value={draft.idle_max_minutes}
                            onValueChange={(value) =>
                                setDraft((current) => ({ ...current, idle_max_minutes: Math.round(value) }))
                            }
                            parseAs="int"
                            min={1}
                            max={10080}
                            disabled={saving}
                        />
                        <span className="settings-field__suffix">{t('settingsManagement.idleGreeting.minutes')}</span>
                    </SettingsField>
                </Group>

                <Group title={t('settings.idleGreetingWindows')} footer={t('settings.idleGreetingWindowsHint')}>
                    {draft.time_windows.map((window, index) => (
                        <div className="idle-window-row" key={index}>
                            <input
                                className="settings-input"
                                aria-label={`${t('settings.idleGreetingWindowStart')} ${index + 1}`}
                                value={window.start}
                                onChange={(event) => handleWindowChange(index, { start: event.target.value })}
                                placeholder="09:00"
                                inputMode="numeric"
                                disabled={saving}
                            />
                            <span className="idle-window-row__sep">{t('settingsManagement.idleGreeting.to')}</span>
                            <input
                                className="settings-input"
                                aria-label={`${t('settings.idleGreetingWindowEnd')} ${index + 1}`}
                                value={window.end}
                                onChange={(event) => handleWindowChange(index, { end: event.target.value })}
                                placeholder="22:00"
                                inputMode="numeric"
                                disabled={saving}
                            />
                            <button
                                type="button"
                                className="ui-icon-btn"
                                aria-label={`${t('settingsManagement.idleGreeting.removeWindow')} ${index + 1}`}
                                title={t('settingsManagement.idleGreeting.removeWindow')}
                                onClick={() => handleRemoveWindow(index)}
                                disabled={saving || draft.time_windows.length <= 1}
                            >
                                <Icon name="trash" />
                            </button>
                        </div>
                    ))}
                    <Row
                        className="idle-window-add"
                        icon="plus"
                        label={t('settingsManagement.idleGreeting.addWindow')}
                        detail={t('settings.idleGreetingWindowFormat')}
                        chevron={false}
                        disabled={saving}
                        onClick={handleAddWindow}
                    />
                </Group>

                {showValidationErrors && validationError && (
                    <p className="settings-inline-error" role="alert">
                        {validationError}
                    </p>
                )}
            </div>
        </motion.div>
    )
}

export default IdleGreetingSettingsPanel
