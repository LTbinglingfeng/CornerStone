import type { ApiResponse } from '../types/chat'
import type { Reminder } from '../types/reminder'
import { translate } from '../i18n'
import { apiFetchJson } from './api'

export const reminderService = {
    async listReminders(): Promise<Reminder[]> {
        const data = await apiFetchJson<ApiResponse<Reminder[]>>('/api/settings/reminders')
        if (!data.success) {
            throw new Error(data.error || translate('service.listRemindersFailed'))
        }
        return data.data || []
    },

    async getReminder(id: string): Promise<Reminder> {
        const data = await apiFetchJson<ApiResponse<Reminder>>(`/api/settings/reminders/${encodeURIComponent(id)}`)
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.getReminderFailed'))
        }
        return data.data
    },

    async updateReminder(
        id: string,
        update: {
            title?: string
            reminder_prompt?: string
            due_at?: string
        }
    ): Promise<Reminder> {
        const data = await apiFetchJson<ApiResponse<Reminder>>(`/api/settings/reminders/${encodeURIComponent(id)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(update),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.saveReminderFailed'))
        }
        return data.data
    },

    async cancelReminder(id: string): Promise<Reminder> {
        const data = await apiFetchJson<ApiResponse<Reminder>>(
            `/api/settings/reminders/${encodeURIComponent(id)}/cancel`,
            {
                method: 'POST',
            }
        )
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.cancelReminderFailed'))
        }
        return data.data
    },

    async deleteReminder(id: string): Promise<void> {
        const data = await apiFetchJson<ApiResponse<string>>(`/api/settings/reminders/${encodeURIComponent(id)}`, {
            method: 'DELETE',
        })
        if (!data.success) {
            throw new Error(data.error || translate('service.deleteReminderFailed'))
        }
    },
}
