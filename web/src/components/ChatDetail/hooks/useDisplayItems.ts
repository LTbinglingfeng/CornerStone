import { useMemo } from 'react'
import type { ChatMessage } from '../../../types/chat'
import type { DisplayItem } from '../types'
import {
    getAssistantMessageSplitToken,
    isRecalledMessage,
    normalizeAssistantContent,
    splitAssistantMessageContent,
} from '../utils'

interface UseDisplayItemsOptions {
    messages: ChatMessage[]
    transientErrors?: ChatMessage[]
    sending: boolean
    activeToolCallMessage: ChatMessage | null
    streamingTimestamp: string | null
    revealingTimestamp: string | null
    visibleSegments: number
}

/** Tools that already render as their own bubble or banner instead of a generic tool-call card. */
const TOOLS_WITH_DEDICATED_UI = new Set(['send_red_packet', 'send_pat', 'red_packet_received'])

export function useDisplayItems(options: UseDisplayItemsOptions): DisplayItem[] {
    const {
        messages,
        transientErrors,
        sending,
        activeToolCallMessage,
        streamingTimestamp,
        revealingTimestamp,
        visibleSegments,
    } = options

    return useMemo(() => {
        const items: DisplayItem[] = []
        // Call IDs are only local to a tool step (Gemini can reuse its fallback IDs).
        // Match the nearest preceding call in this turn, allowing interleaved receipt banners.
        const resultsByMessage = new Map<ChatMessage, Map<string, string>>()
        const pendingCalls = new Map<string, ChatMessage>()
        messages.forEach((message) => {
            if (message.role !== 'tool') {
                if (message.role !== 'assistant' || !message.tool_calls?.length) pendingCalls.clear()
                if (message.tool_calls?.length) {
                    resultsByMessage.set(message, new Map())
                    message.tool_calls.forEach((call) => {
                        if (call.id) pendingCalls.set(call.id, message)
                    })
                }
                return
            }
            const id = message.tool_call_id
            const callMessage = id ? pendingCalls.get(id) : undefined
            if (callMessage && id) {
                resultsByMessage.get(callMessage)!.set(id, message.content || '')
                pendingCalls.delete(id)
            }
        })

        messages.forEach((message, index) => {
            if (message.role === 'tool') return

            const hasImages = !!(message.image_paths && message.image_paths.length > 0)
            const toolCalls = message.tool_calls || []
            const toolResults = resultsByMessage.get(message)
            const successfulNoReplyToolCallIds = new Set<string>()
            const attachmentIntentToolCallIds = new Set<string>()
            toolResults?.forEach((result, id) => {
                try {
                    const payload = JSON.parse(result || '{}') as { ok?: boolean; tool?: string } | null
                    if (payload?.tool === 'send_attachment' && payload.ok === true) {
                        attachmentIntentToolCallIds.add(id)
                    }
                    if (payload?.tool === 'no_reply' && payload.ok === true) {
                        successfulNoReplyToolCallIds.add(id)
                    }
                } catch {
                    // Malformed results still belong to this call and are shown verbatim.
                }
            })
            const supportedCalls = toolCalls.filter(
                (tc) =>
                    tc.function.name === 'send_red_packet' ||
                    tc.function.name === 'send_pat' ||
                    tc.function.name === 'red_packet_received'
            )

            const isAssistant = message.role === 'assistant'
            const isAssistantToolCallMessage = isAssistant && toolCalls.length > 0
            const isStreamingAssistantMessage =
                isAssistant && sending && streamingTimestamp !== null && message.timestamp === streamingTimestamp
            const messageSplitToken = isAssistant ? getAssistantMessageSplitToken(message) : ''

            let assistantSegments =
                isAssistant && !isAssistantToolCallMessage
                    ? splitAssistantMessageContent(message.content, messageSplitToken)
                    : []
            const normalizedContent =
                isAssistant && !isAssistantToolCallMessage ? normalizeAssistantContent(message.content) : ''
            const hasSplitToken =
                isAssistant && messageSplitToken !== '' && normalizedContent.includes(messageSplitToken)
            const endsWithSplitToken =
                isAssistant && messageSplitToken !== '' && normalizedContent.trimEnd().endsWith(messageSplitToken)

            const shouldHoldTrailingSegment =
                isStreamingAssistantMessage && hasSplitToken && !endsWithSplitToken && assistantSegments.length > 1

            if (shouldHoldTrailingSegment) {
                assistantSegments = assistantSegments.slice(0, -1)
            }

            const isRevealingAssistantMessage =
                isAssistant && revealingTimestamp !== null && message.timestamp === revealingTimestamp
            if (isRevealingAssistantMessage) {
                assistantSegments = assistantSegments.slice(0, visibleSegments)
            }

            const hasText = isAssistant ? assistantSegments.length > 0 : message.content.trim() !== ''
            const hasContent =
                hasText || hasImages || !!message.attachments?.length || !!message.tts_audio_paths?.length

            if (hasContent) {
                if (isAssistant) {
                    if (assistantSegments.length > 0) {
                        assistantSegments.forEach((segment, segmentIndex) => {
                            const ttsPath = message.tts_audio_paths?.[segmentIndex]
                            const segmentMessage: ChatMessage = {
                                ...message,
                                content: segment,
                                ...(segmentIndex === 0 ? {} : { image_paths: undefined, attachments: undefined }),
                                ...(ttsPath ? { tts_audio_path: ttsPath } : {}),
                            }
                            items.push({
                                key: `${message.timestamp}-text-${segmentIndex}`,
                                role: message.role,
                                type: 'text',
                                message: segmentMessage,
                                messageIndex: index,
                            })
                        })
                    } else {
                        items.push({
                            key: `${message.timestamp}-text-0`,
                            role: message.role,
                            type: 'text',
                            message: { ...message, content: '', tts_audio_path: message.tts_audio_paths?.[0] },
                            messageIndex: index,
                        })
                    }
                } else {
                    if (isRecalledMessage(message)) {
                        items.push({
                            key: `${message.timestamp}-recalled`,
                            role: message.role,
                            type: 'recall-banner',
                            message,
                            messageIndex: index,
                        })
                    } else {
                        items.push({
                            key: `${message.timestamp}-text`,
                            role: message.role,
                            type: 'text',
                            message,
                            messageIndex: index,
                        })
                    }
                }
            }

            supportedCalls.forEach((toolCall, toolIndex) => {
                if (toolCall.function.name === 'send_red_packet') {
                    items.push({
                        key: `${message.timestamp}-rp-${toolCall.id || toolIndex}`,
                        role: message.role,
                        type: 'red-packet',
                        message,
                        toolCall,
                        messageIndex: index,
                    })
                }
                if (toolCall.function.name === 'send_pat') {
                    items.push({
                        key: `${message.timestamp}-pat-${toolCall.id || toolIndex}`,
                        role: message.role,
                        type: 'pat-banner',
                        message,
                        toolCall,
                        messageIndex: index,
                    })
                }
                if (toolCall.function.name === 'red_packet_received') {
                    items.push({
                        key: `${message.timestamp}-rp-received-${toolCall.id || toolIndex}`,
                        role: message.role,
                        type: 'red-packet-received-banner',
                        message,
                        toolCall,
                        messageIndex: index,
                    })
                }
            })

            toolCalls.forEach((toolCall, toolIndex) => {
                const name = toolCall.function.name
                if (name === 'send_attachment' && attachmentIntentToolCallIds.has(toolCall.id)) {
                    items.push({
                        key: `${message.timestamp}-attachment-intent-${toolCall.id}`,
                        role: message.role,
                        type: 'attachment-intent-banner',
                        message,
                        toolCall,
                        messageIndex: index,
                    })
                    return
                }
                // A successful no_reply already shows its own banner below.
                if (name === 'no_reply' && successfulNoReplyToolCallIds.has(toolCall.id)) return
                if (!isAssistant || TOOLS_WITH_DEDICATED_UI.has(name)) return

                const result = toolCall.id ? toolResults?.get(toolCall.id) : undefined
                items.push({
                    key: `${message.timestamp}-tool-${toolCall.id || toolIndex}`,
                    role: message.role,
                    type: 'tool-call',
                    message,
                    toolCall,
                    result,
                    isRunning: sending && message === activeToolCallMessage && result === undefined,
                    messageIndex: index,
                })
            })

            const matchedNoReplyCall = toolCalls.find(
                (toolCall) => toolCall.function.name === 'no_reply' && successfulNoReplyToolCallIds.has(toolCall.id)
            )
            if (matchedNoReplyCall) {
                items.push({
                    key: `${message.timestamp}-no-reply-${matchedNoReplyCall.id}`,
                    role: message.role,
                    type: 'no-reply-banner',
                    message,
                    toolCall: matchedNoReplyCall,
                    messageIndex: index,
                })
            }
        })
        // Merge only at the display layer. Persisted message indices stay untouched.
        transientErrors?.forEach((message, index) => {
            const item: DisplayItem = {
                key: `error-${message.timestamp}-${index}`,
                role: 'assistant',
                type: 'error',
                message,
                messageIndex: -1,
            }
            const nextIndex = items.findIndex(
                (existing) => Date.parse(existing.message.timestamp) > Date.parse(message.timestamp)
            )
            items.splice(nextIndex < 0 ? items.length : nextIndex, 0, item)
        })
        return items
    }, [
        messages,
        transientErrors,
        sending,
        activeToolCallMessage,
        streamingTimestamp,
        revealingTimestamp,
        visibleSegments,
    ])
}
