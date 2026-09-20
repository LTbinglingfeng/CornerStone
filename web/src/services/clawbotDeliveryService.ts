import type { ApiResponse } from '../types/chat'
import { translate } from '../i18n'
import { apiFetchJson } from './api'

export type DeliveryAction = 'retry' | 'confirm_sent' | 'cancel'
export interface DeliveryJob {
    id: string
    account_id: string
    revision: number
    current_chunk_id?: string
    history_pending: boolean
    kind: 'chat' | 'command' | 'reminder' | 'greeting' | 'direct'
    user_id: string
    session_id?: string
    status:
        | 'queued'
        | 'preparing'
        | 'generating'
        | 'executing'
        | 'ready'
        | 'sending'
        | 'uncertain'
        | 'blocked'
        | 'committing'
        | 'done'
        | 'cancelled'
    phase?: 'preparation' | 'generation' | 'command' | 'send' | 'history'
    created_at: string
    updated_at: string
    last_error?: string
    next_attempt_at?: string
    chunks: { text: string; status: 'pending' | 'sending' | 'sent' | 'uncertain'; message_id?: string }[]
    allowed_actions: string[]
}

type DeliveryUpdate = {
    id: string
    account_id: string
    revision: number
    action: DeliveryAction
    chunk_id?: string
    operation_id: string
}

async function request(signal: AbortSignal, update?: DeliveryUpdate): Promise<DeliveryJob[]> {
    const response = await apiFetchJson<ApiResponse<{ jobs: DeliveryJob[] }>>('/api/settings/clawbot/deliveries', {
        signal,
        ...(update
            ? {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify(update),
              }
            : {}),
    })
    if (!response.success || !response.data || !Array.isArray(response.data.jobs)) {
        // Do not surface arbitrary response bodies that may contain credentials or raw requests.
        throw new Error(translate(update ? 'delivery.actionFailed' : 'delivery.loadFailed'))
    }
    return response.data.jobs
}

export const clawbotDeliveryService = {
    list: (signal: AbortSignal) => request(signal),
    act: (job: DeliveryJob, action: DeliveryAction, signal: AbortSignal) =>
        request(signal, {
            id: job.id,
            account_id: job.account_id,
            revision: job.revision,
            chunk_id: job.current_chunk_id,
            action,
            operation_id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`,
        }),
}
