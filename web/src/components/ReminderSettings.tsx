import { useEffect, useState } from 'react'
import { motion } from 'motion/react'
import { reminderService } from '../services/reminderService'
import type { Reminder } from '../types/reminder'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { drawerVariants } from '../utils/motion'
import { Badge, EmptyState, Group, Icon, Row, type Tone } from './ui'
import { SettingsField } from './SettingsField'
import './ProviderSettings.css'
import './ReminderSettings.css'

interface ReminderSettingsProps {
    onBack: () => void
}

const STATUS_TONES: Record<string, Tone> = {
    pending: 'info',
    firing: 'warning',
    sent: 'success',
    failed: 'danger',
    cancelled: 'neutral',
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
    const isPending =
        selectedReminder?.status === 'pending' ||
        (selectedReminder?.channel === 'clawbot' && selectedReminder.status === 'firing')

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
        if (channel === 'napcat') {
            return t('settings.reminderChannelNapCat')
        }
        return t('settings.reminderChannelWeb')
    }

    const getTargetLabel = (reminder: Reminder) => {
        const userID = reminder.target?.user_id || reminder.clawbot_user_id || ''
        const botSelfID = reminder.target?.bot_self_id || ''
        if (!userID && !botSelfID) return t('common.notSet')
        if (reminder.channel === 'napcat') {
            if (userID && botSelfID) {
                return `${userID} · ${t('napCat.selfId')}: ${botSelfID}`
            }
            if (userID) return userID
            return `${t('napCat.selfId')}: ${botSelfID}`
        }
        return userID || botSelfID
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

    const metaRows: Array<{ label: string; value: React.ReactNode }> = selectedReminder
        ? [
              { label: t('settings.reminderChannel'), value: getChannelLabel(selectedReminder.channel) },
              { label: t('settings.persona'), value: selectedReminder.prompt_name || selectedReminder.prompt_id },
              {
                  label: t('settings.reminderSession'),
                  value: selectedReminder.session_title || selectedReminder.session_id,
              },
              ...(selectedReminder.channel === 'clawbot' || selectedReminder.channel === 'napcat'
                  ? [{ label: t('settings.reminderTarget'), value: getTargetLabel(selectedReminder) }]
                  : []),
              { label: t('settings.reminderAttempts'), value: selectedReminder.attempts },
              { label: t('settings.reminderCreatedAt'), value: formatDateTime(selectedReminder.created_at) },
              { label: t('settings.reminderUpdatedAt'), value: formatDateTime(selectedReminder.updated_at) },
              ...(selectedReminder.fired_at
                  ? [{ label: t('settings.reminderFiredAt'), value: formatDateTime(selectedReminder.fired_at) }]
                  : []),
          ]
        : []

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
                <div className="provider-settings-title">
                    {isDetailView ? t('settings.reminderDetails') : t('settings.reminders')}
                </div>
                {isDetailView && isPending && (
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
            ) : isDetailView && selectedReminder ? (
                <div className="provider-settings-content">
                    <header className="reminder-detail-header">
                        <h2>{selectedReminder.title}</h2>
                        <Badge tone={STATUS_TONES[selectedReminder.status] || 'neutral'} dot>
                            {getStatusLabel(selectedReminder.status)}
                        </Badge>
                    </header>

                    <Group title={t('settingsManagement.reminders.content')}>
                        {isPending ? (
                            <>
                                <SettingsField label={t('settings.reminderTitle')} stacked>
                                    <input
                                        className="settings-input"
                                        aria-label={t('settings.reminderTitle')}
                                        value={editingTitle}
                                        onChange={(event) => setEditingTitle(event.target.value)}
                                        disabled={saving}
                                    />
                                </SettingsField>
                                <SettingsField
                                    label={t('settings.reminderDueAt')}
                                    hint={t('settings.reminderDueAtHint')}
                                    stacked
                                >
                                    <input
                                        className="settings-input"
                                        aria-label={t('settings.reminderDueAt')}
                                        value={editingDueAt}
                                        onChange={(event) => setEditingDueAt(event.target.value)}
                                        placeholder="2026-04-05T18:30:00+08:00"
                                        disabled={saving}
                                    />
                                </SettingsField>
                                <SettingsField label={t('settings.reminderPrompt')} stacked>
                                    <textarea
                                        className="settings-textarea"
                                        aria-label={t('settings.reminderPrompt')}
                                        value={editingPrompt}
                                        onChange={(event) => setEditingPrompt(event.target.value)}
                                        rows={8}
                                        disabled={saving}
                                    />
                                </SettingsField>
                            </>
                        ) : (
                            <>
                                <Row label={t('settings.reminderTitle')} value={selectedReminder.title} />
                                <Row
                                    label={t('settings.reminderDueAt')}
                                    value={formatDateTime(selectedReminder.due_at)}
                                />
                                <SettingsField label={t('settings.reminderPrompt')} stacked>
                                    <div className="reminder-readonly">{selectedReminder.reminder_prompt}</div>
                                </SettingsField>
                            </>
                        )}
                    </Group>

                    <Group
                        title={t('settingsManagement.reminders.details')}
                        footer={
                            selectedReminder.channel === 'clawbot' && selectedReminder.status === 'firing'
                                ? t('settings.reminderDeliveryInFlightHint')
                                : undefined
                        }
                    >
                        {metaRows.map((row) => (
                            <Row key={row.label} label={row.label} value={row.value} />
                        ))}
                        {selectedReminder.last_error && (
                            <SettingsField label={t('settings.reminderLastError')} stacked>
                                <div className="reminder-readonly reminder-error-readonly">
                                    {selectedReminder.last_error}
                                </div>
                            </SettingsField>
                        )}
                    </Group>

                    <div className="reminder-action-row">
                        {isPending ? (
                            <button
                                type="button"
                                className="ui-btn ui-btn--danger"
                                onClick={() => void handleCancelReminder()}
                                disabled={saving}
                            >
                                {t('settings.cancelReminder')}
                            </button>
                        ) : (
                            <button
                                type="button"
                                className="ui-btn ui-btn--danger"
                                onClick={() => void handleDeleteReminder()}
                                disabled={saving}
                            >
                                <Icon name="trash" />
                                {t('common.delete')}
                            </button>
                        )}
                    </div>
                </div>
            ) : (
                <div className="provider-settings-content">
                    {reminders.length === 0 ? (
                        <EmptyState
                            icon="bell"
                            title={t('settings.noReminders')}
                            description={t('settingsManagement.reminders.emptyHint')}
                        />
                    ) : (
                        <Group title={t('settings.reminders')} description={t('settings.reminderManagerHint')}>
                            {reminders.map((reminder) => (
                                <Row
                                    key={reminder.id}
                                    className="reminder-row"
                                    label={reminder.title}
                                    detail={[
                                        formatDateTime(reminder.due_at),
                                        getChannelLabel(reminder.channel),
                                        reminder.prompt_name || reminder.prompt_id,
                                        reminder.session_title || reminder.session_id,
                                    ]
                                        .filter(Boolean)
                                        .join(' · ')}
                                    trailing={
                                        <Badge tone={STATUS_TONES[reminder.status] || 'neutral'} dot>
                                            {getStatusLabel(reminder.status)}
                                        </Badge>
                                    }
                                    onClick={() => void handleOpenReminder(reminder.id)}
                                />
                            ))}
                        </Group>
                    )}
                </div>
            )}
        </motion.div>
    )
}

export default ReminderSettingsPanel
