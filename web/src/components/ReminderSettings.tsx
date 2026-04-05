import { useEffect, useState } from 'react'
import { motion } from 'motion/react'
import { reminderService } from '../services/reminderService'
import type { Reminder } from '../types/reminder'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { drawerVariants } from '../utils/motion'
import './ProviderSettings.css'
import './Settings.css'
import './ReminderSettings.css'

interface ReminderSettingsProps {
    onBack: () => void
}

const ReminderSettingsPanel: React.FC<ReminderSettingsProps> = ({ onBack }) => {
    const { t, locale } = useT()
    const { showToast } = useToast()

    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [reminders, setReminders] = useState<Reminder[]>([])
    const [selectedReminder, setSelectedReminder] = useState<Reminder | null>(null)
    const [editingTitle, setEditingTitle] = useState('')
    const [editingDueAt, setEditingDueAt] = useState('')
    const [editingPrompt, setEditingPrompt] = useState('')

    const isDetailView = selectedReminder !== null
    const isPending = selectedReminder?.status === 'pending'

    const formatDateTime = (value?: string) => {
        if (!value) return t('common.notSet')
        const date = new Date(value)
        if (Number.isNaN(date.getTime())) return value
        return date.toLocaleString(locale === 'en' ? 'en-US' : 'zh-CN', {
            hour12: false,
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
        })
    }

    const getStatusLabel = (status: string) => {
        const labels: Record<string, string> = {
            pending: t('settings.reminderStatusPending'),
            firing: t('settings.reminderStatusFiring'),
            sent: t('settings.reminderStatusSent'),
            failed: t('settings.reminderStatusFailed'),
            cancelled: t('settings.reminderStatusCancelled'),
        }
        return labels[status] || status
    }

    const getChannelLabel = (channel: string) => {
        if (channel === 'clawbot') {
            return t('settings.reminderChannelClawBot')
        }
        return t('settings.reminderChannelWeb')
    }

    const syncEditor = (reminder: Reminder) => {
        setSelectedReminder(reminder)
        setEditingTitle(reminder.title)
        setEditingDueAt(reminder.due_at)
        setEditingPrompt(reminder.reminder_prompt)
    }

    const loadReminders = async () => {
        setLoading(true)
        try {
            const data = await reminderService.listReminders()
            setReminders(data)
        } catch (error) {
            showToast(error instanceof Error ? error.message : t('common.loadFailed'), 'error')
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        void loadReminders()
    }, [])

    const handleBack = () => {
        if (isDetailView) {
            setSelectedReminder(null)
            return
        }
        onBack()
    }

    const handleOpenReminder = async (id: string) => {
        try {
            const reminder = await reminderService.getReminder(id)
            syncEditor(reminder)
        } catch (error) {
            showToast(error instanceof Error ? error.message : t('common.loadFailed'), 'error')
        }
    }

    const updateReminderInList = (updated: Reminder) => {
        setReminders((current) => current.map((item) => (item.id === updated.id ? updated : item)))
        syncEditor(updated)
    }

    const handleSave = async () => {
        if (!selectedReminder || !isPending || saving) return
        setSaving(true)
        try {
            const updated = await reminderService.updateReminder(selectedReminder.id, {
                title: editingTitle,
                due_at: editingDueAt,
                reminder_prompt: editingPrompt,
            })
            updateReminderInList(updated)
            showToast(t('settings.reminderSaved'), 'success')
        } catch (error) {
            showToast(error instanceof Error ? error.message : t('common.saveFailed'), 'error')
        } finally {
            setSaving(false)
        }
    }

    const handleCancelReminder = async () => {
        if (!selectedReminder || !isPending || saving) return
        setSaving(true)
        try {
            const updated = await reminderService.cancelReminder(selectedReminder.id)
            updateReminderInList(updated)
            showToast(t('settings.reminderCancelled'), 'success')
        } catch (error) {
            showToast(error instanceof Error ? error.message : t('common.operationFailed'), 'error')
        } finally {
            setSaving(false)
        }
    }

    const handleDeleteReminder = async () => {
        if (!selectedReminder || saving) return
        setSaving(true)
        try {
            await reminderService.deleteReminder(selectedReminder.id)
            setReminders((current) => current.filter((item) => item.id !== selectedReminder.id))
            setSelectedReminder(null)
            showToast(t('settings.reminderDeleted'), 'success')
        } catch (error) {
            showToast(error instanceof Error ? error.message : t('common.operationFailed'), 'error')
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
                <button className="back-button" onClick={handleBack}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">
                    {isDetailView ? t('settings.reminderDetails') : t('settings.reminders')}
                </div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : isDetailView && selectedReminder ? (
                <div className="provider-settings-content">
                    <div className="settings-section">
                        <h3>{selectedReminder.title}</h3>
                        <div className={`reminder-status-badge ${selectedReminder.status}`}>
                            {getStatusLabel(selectedReminder.status)}
                        </div>
                    </div>

                    <div className="settings-section">
                        <div className="settings-group">
                            <label className="settings-label">{t('settings.reminderTitle')}</label>
                            {isPending ? (
                                <input
                                    className="settings-input"
                                    value={editingTitle}
                                    onChange={(event) => setEditingTitle(event.target.value)}
                                    disabled={saving}
                                />
                            ) : (
                                <div className="reminder-readonly">{selectedReminder.title}</div>
                            )}
                        </div>

                        <div className="settings-group">
                            <label className="settings-label">{t('settings.reminderDueAt')}</label>
                            {isPending ? (
                                <input
                                    className="settings-input"
                                    value={editingDueAt}
                                    onChange={(event) => setEditingDueAt(event.target.value)}
                                    placeholder="2026-04-05T18:30:00+08:00"
                                    disabled={saving}
                                />
                            ) : (
                                <div className="reminder-readonly">{formatDateTime(selectedReminder.due_at)}</div>
                            )}
                            {isPending && <p className="prompt-modal-hint">{t('settings.reminderDueAtHint')}</p>}
                        </div>

                        <div className="settings-group">
                            <label className="settings-label">{t('settings.reminderPrompt')}</label>
                            {isPending ? (
                                <textarea
                                    className="settings-textarea"
                                    value={editingPrompt}
                                    onChange={(event) => setEditingPrompt(event.target.value)}
                                    rows={8}
                                    disabled={saving}
                                />
                            ) : (
                                <div className="reminder-readonly reminder-prompt-readonly">
                                    {selectedReminder.reminder_prompt}
                                </div>
                            )}
                        </div>
                    </div>

                    <div className="settings-section">
                        <div className="reminder-meta-list">
                            <div className="reminder-meta-row">
                                <span>{t('settings.reminderChannel')}</span>
                                <strong>{getChannelLabel(selectedReminder.channel)}</strong>
                            </div>
                            <div className="reminder-meta-row">
                                <span>{t('settings.persona')}</span>
                                <strong>{selectedReminder.prompt_name || selectedReminder.prompt_id}</strong>
                            </div>
                            <div className="reminder-meta-row">
                                <span>{t('settings.reminderSession')}</span>
                                <strong>{selectedReminder.session_title || selectedReminder.session_id}</strong>
                            </div>
                            {selectedReminder.channel === 'clawbot' && (
                                <div className="reminder-meta-row">
                                    <span>{t('settings.reminderTarget')}</span>
                                    <strong>{selectedReminder.clawbot_user_id || t('common.notSet')}</strong>
                                </div>
                            )}
                            <div className="reminder-meta-row">
                                <span>{t('settings.reminderAttempts')}</span>
                                <strong>{selectedReminder.attempts}</strong>
                            </div>
                            <div className="reminder-meta-row">
                                <span>{t('settings.reminderCreatedAt')}</span>
                                <strong>{formatDateTime(selectedReminder.created_at)}</strong>
                            </div>
                            <div className="reminder-meta-row">
                                <span>{t('settings.reminderUpdatedAt')}</span>
                                <strong>{formatDateTime(selectedReminder.updated_at)}</strong>
                            </div>
                            {selectedReminder.fired_at && (
                                <div className="reminder-meta-row">
                                    <span>{t('settings.reminderFiredAt')}</span>
                                    <strong>{formatDateTime(selectedReminder.fired_at)}</strong>
                                </div>
                            )}
                        </div>
                        {selectedReminder.last_error && (
                            <div className="settings-group" style={{ marginTop: 16 }}>
                                <label className="settings-label">{t('settings.reminderLastError')}</label>
                                <div className="reminder-readonly reminder-error-readonly">
                                    {selectedReminder.last_error}
                                </div>
                            </div>
                        )}
                    </div>

                    <div className="reminder-action-row">
                        {isPending ? (
                            <>
                                <button className="settings-save-btn" onClick={handleSave} disabled={saving}>
                                    {saving ? t('common.saving') : t('common.save')}
                                </button>
                                <button
                                    className="reminder-secondary-btn"
                                    onClick={handleCancelReminder}
                                    disabled={saving}
                                >
                                    {t('settings.cancelReminder')}
                                </button>
                            </>
                        ) : (
                            <button
                                className="reminder-secondary-btn danger"
                                onClick={handleDeleteReminder}
                                disabled={saving}
                            >
                                {t('common.delete')}
                            </button>
                        )}
                    </div>
                </div>
            ) : (
                <div className="provider-settings-content">
                    <div className="settings-section">
                        <h3>{t('settings.reminders')}</h3>
                        <p className="prompt-modal-hint">{t('settings.reminderManagerHint')}</p>
                    </div>

                    {reminders.length === 0 ? (
                        <div className="settings-section reminder-empty">{t('settings.noReminders')}</div>
                    ) : (
                        <div className="reminder-list">
                            {reminders.map((reminder) => (
                                <button
                                    key={reminder.id}
                                    className="reminder-card"
                                    onClick={() => void handleOpenReminder(reminder.id)}
                                >
                                    <div className="reminder-card-header">
                                        <div className="reminder-card-title-block">
                                            <div className="reminder-card-title">{reminder.title}</div>
                                            <div className="reminder-card-subtitle">
                                                {reminder.prompt_name || reminder.prompt_id}
                                            </div>
                                        </div>
                                        <div className={`reminder-status-badge ${reminder.status}`}>
                                            {getStatusLabel(reminder.status)}
                                        </div>
                                    </div>
                                    <div className="reminder-card-meta">
                                        <div>{formatDateTime(reminder.due_at)}</div>
                                        <div>{getChannelLabel(reminder.channel)}</div>
                                        <div>{reminder.session_title || reminder.session_id}</div>
                                        {reminder.channel === 'clawbot' && reminder.clawbot_user_id && (
                                            <div>{reminder.clawbot_user_id}</div>
                                        )}
                                    </div>
                                </button>
                            ))}
                        </div>
                    )}
                </div>
            )}
        </motion.div>
    )
}

export default ReminderSettingsPanel
