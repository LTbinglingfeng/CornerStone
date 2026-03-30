import type { ChatMessage, ToolCall } from '../../../types/chat'
import { getLocale, translate } from '../../../i18n'
import type { RedPacketReceivedRecord } from './types'

export const normalizePacketKey = (rawKey: string) => rawKey.replace(/[^a-zA-Z0-9_-]/g, '_')

export const derivePacketKeys = (toolCall: ToolCall, rawKey: string) => {
    const legacyKey = normalizePacketKey(rawKey)
    const primaryKey =
        typeof toolCall.id === 'string' && toolCall.id.trim() !== ''
            ? normalizePacketKey(toolCall.id.trim())
            : legacyKey
    return { primaryKey, legacyKey }
}

export const collectOpenedRedPacketKeys = (messages: ChatMessage[]): Set<string> => {
    const keys = new Set<string>()
    messages.forEach((message) => {
        const toolCalls = message.tool_calls || []
        toolCalls.forEach((toolCall) => {
            if (toolCall.function.name !== 'red_packet_received') return
            try {
                const args = JSON.parse(toolCall.function.arguments || '{}') as { packet_key?: unknown }
                const packetKey = typeof args.packet_key === 'string' ? args.packet_key.trim() : ''
                if (packetKey) keys.add(packetKey)
            } catch {
                // ignore invalid payload
            }
        })
    })
    return keys
}

export const getRedPacketReceivedRecord = (
    messages: ChatMessage[],
    packetKey: string
): RedPacketReceivedRecord | null => {
    const normalizedTarget = normalizePacketKey(packetKey)
    for (const message of messages) {
        const toolCalls = message.tool_calls || []
        for (const toolCall of toolCalls) {
            if (toolCall.function.name !== 'red_packet_received') continue
            try {
                const args = JSON.parse(toolCall.function.arguments || '{}') as {
                    packet_key?: unknown
                    receiver_name?: unknown
                    sender_name?: unknown
                }
                const rawKey = typeof args.packet_key === 'string' ? args.packet_key.trim() : ''
                if (!rawKey) continue
                if (normalizePacketKey(rawKey) !== normalizedTarget) continue

                const receiverName =
                    typeof args.receiver_name === 'string' && args.receiver_name.trim() !== ''
                        ? args.receiver_name.trim()
                        : ''
                const senderName =
                    typeof args.sender_name === 'string' && args.sender_name.trim() !== ''
                        ? args.sender_name.trim()
                        : ''

                return { receiverName, senderName, timestamp: message.timestamp }
            } catch {
                // ignore invalid payload
            }
        }
    }
    return null
}

export const formatRedPacketTime = (timestamp: string) => {
    const date = new Date(timestamp)
    if (!Number.isFinite(date.getTime())) return ''
    const locale = getLocale()
    if (locale === 'en') {
        return `${date.getMonth() + 1}/${date.getDate()} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
    }
    const month = date.getMonth() + 1
    const day = date.getDate()
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')
    return `${month}${translate('redPacket.month')}${day}${translate('redPacket.day')} ${hours}:${minutes}`
}

export const inferRedPacketParties = (
    messages: ChatMessage[],
    packetKey: string,
    userName: string,
    assistantName: string
): null | { senderRole: 'user' | 'assistant'; senderName: string; receiverName: string } => {
    const normalizedTarget = normalizePacketKey(packetKey)
    for (const message of messages) {
        const toolCalls = message.tool_calls || []
        for (let toolIndex = 0; toolIndex < toolCalls.length; toolIndex++) {
            const toolCall = toolCalls[toolIndex]
            if (toolCall.function.name !== 'send_red_packet') continue
            const rawKey = `${message.timestamp}-rp-${toolCall.id || toolIndex}`
            const { primaryKey, legacyKey } = derivePacketKeys(toolCall, rawKey)
            if (primaryKey !== normalizedTarget && legacyKey !== normalizedTarget) continue

            const senderRole: 'user' | 'assistant' = message.role === 'user' ? 'user' : 'assistant'
            const senderName = senderRole === 'user' ? userName : assistantName
            const receiverName = senderRole === 'user' ? assistantName : userName
            return { senderRole, senderName, receiverName }
        }
    }
    return null
}
