export type ReminderChannel = 'web' | 'clawbot'

export type ReminderStatus = 'pending' | 'firing' | 'sent' | 'failed' | 'cancelled' | string

export interface Reminder {
    id: string
    channel: ReminderChannel
    session_id: string
    session_title?: string
    session_exists: boolean
    prompt_id: string
    prompt_name: string
    prompt_exists: boolean
    clawbot_user_id?: string
    title: string
    reminder_prompt: string
    due_at: string
    status: ReminderStatus
    attempts: number
    last_error?: string
    created_at: string
    updated_at: string
    fired_at?: string
}
