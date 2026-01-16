import { useCallback, useEffect, useRef, useState } from 'react'
import type { ChatMessage, ToolCall } from '../../../types/chat'
import { sendMessage as sendMessageApi, sendMessageBeacon as sendMessageBeaconApi } from '../../../services/api'
import { getReplyWaitWindowConfig } from '../../../utils/replyWaitWindow'
import { useMessageReveal } from './useMessageReveal'

type FlushMode = 'foreground' | 'background'

interface UseMessageStreamOptions {
    sessionId: string
    promptId?: string
    messages: ChatMessage[]
    setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>
    onError?: (error: string) => void
}

interface UseMessageStreamReturn {
    sending: boolean
    streamingTimestamp: string | null
    revealingTimestamp: string | null
    assistantVisibleSegments: number
    sendMessage: (userMessage: ChatMessage) => Promise<void>
    flushPendingMessages: (mode: FlushMode, override?: { sessionId: string; promptId?: string }) => Promise<void>
    abortRequest: () => void
}

export function useMessageStream(options: UseMessageStreamOptions): UseMessageStreamReturn {
    const { sessionId, promptId, messages, setMessages, onError } = options

    const [sending, setSending] = useState(false)
    const [assistantResponseDone, setAssistantResponseDone] = useState(false)
    const [revealingTimestamp, setRevealingTimestamp] = useState<string | null>(null)
    const [assistantVisibleSegments, setAssistantVisibleSegments] = useState(0)
    const [streamingTimestamp, setStreamingTimestamp] = useState<string | null>(null)
    const streamingTimestampRef = useRef<string | null>(null)

    const activeRequestRef = useRef<AbortController | null>(null)
    const pendingOutgoingMessagesRef = useRef<ChatMessage[]>([])
    const pendingOutgoingTimeoutRef = useRef<number | null>(null)
    const lastSessionIdRef = useRef(sessionId)
    const lastPromptIdRef = useRef<string | undefined>(promptId)

    const { reset: resetReveal } = useMessageReveal({
        messages,
        sending,
        streamingTimestamp,
        assistantResponseDone,
        setAssistantResponseDone,
        revealingTimestamp,
        setRevealingTimestamp,
        assistantVisibleSegments,
        setAssistantVisibleSegments,
        onSendingFinished: () => setSending(false),
    })

    const setStreamingTimestampState = useCallback((next: string | null) => {
        streamingTimestampRef.current = next
        setStreamingTimestamp(next)
    }, [])

    const clearPendingOutgoingTimeout = useCallback(() => {
        if (pendingOutgoingTimeoutRef.current !== null) {
            window.clearTimeout(pendingOutgoingTimeoutRef.current)
            pendingOutgoingTimeoutRef.current = null
        }
    }, [])

    const buildSendPayloadMessages = useCallback(
        (outgoingMessages: ChatMessage[]) =>
            outgoingMessages.map(({ role, content, image_paths, tool_calls }) => ({
                role,
                content,
                ...(image_paths ? { image_paths } : {}),
                ...(tool_calls ? { tool_calls } : {}),
            })),
        []
    )

    const flushPendingMessagesOnExit = useCallback(() => {
        const pendingMessages = pendingOutgoingMessagesRef.current
        if (pendingMessages.length === 0) return

        pendingOutgoingMessagesRef.current = []
        clearPendingOutgoingTimeout()

        sendMessageBeaconApi(lastSessionIdRef.current, buildSendPayloadMessages(pendingMessages), {
            promptId: lastPromptIdRef.current,
            stream: false,
        })
    }, [buildSendPayloadMessages, clearPendingOutgoingTimeout])

    const flushPendingMessages = useCallback(
        async (mode: FlushMode, override?: { sessionId: string; promptId?: string }) => {
            if (mode === 'foreground' && sending) return

            const pendingMessages = pendingOutgoingMessagesRef.current
            if (pendingMessages.length === 0) return

            pendingOutgoingMessagesRef.current = []
            clearPendingOutgoingTimeout()

            const targetSessionId = override?.sessionId || sessionId
            const targetPromptId = override?.promptId || promptId

            if (mode === 'background') {
                try {
                    await sendMessageApi(targetSessionId, buildSendPayloadMessages(pendingMessages), {
                        promptId: targetPromptId,
                        stream: false,
                    })
                } catch {
                    // ignore background errors
                }
                return
            }

            resetReveal()
            setAssistantResponseDone(false)
            setRevealingTimestamp(null)
            setAssistantVisibleSegments(0)
            setStreamingTimestampState(null)
            setSending(true)

            let aborted = false
            try {
                activeRequestRef.current?.abort()
                const abortController = new AbortController()
                activeRequestRef.current = abortController

                const response = await sendMessageApi(targetSessionId, buildSendPayloadMessages(pendingMessages), {
                    promptId: targetPromptId,
                    signal: abortController.signal,
                })

                const contentType = response.headers.get('Content-Type') || ''
                const isStreaming = contentType.includes('text/event-stream')

                if (isStreaming) {
                    const reader = response.body?.getReader()
                    const decoder = new TextDecoder()

                    if (!reader) {
                        throw new Error('No response body')
                    }

                    let assistantContent = ''
                    let reasoningContent = ''
                    const toolCallsMap: Map<number, { id: string; type: string; name: string; arguments: string }> =
                        new Map()
                    const assistantTimestamp = new Date().toISOString()
                    setStreamingTimestampState(assistantTimestamp)
                    setRevealingTimestamp(assistantTimestamp)

                    const assistantMessage: ChatMessage = {
                        role: 'assistant',
                        content: '',
                        timestamp: assistantTimestamp,
                    }

                    setMessages((prev) => [...prev, assistantMessage])

                    let buffer = ''
                    let sseDone = false

                    const applyStreamUpdate = () => {
                        const toolCalls: ToolCall[] = Array.from(toolCallsMap.values()).map((tc) => ({
                            id: tc.id,
                            type: tc.type,
                            function: {
                                name: tc.name,
                                arguments: tc.arguments,
                            },
                        }))

                        setMessages((prev) => {
                            const next = [...prev]
                            for (let i = next.length - 1; i >= 0; i--) {
                                const msg = next[i]
                                if (msg.role === 'assistant' && msg.timestamp === assistantTimestamp) {
                                    next[i] = {
                                        ...msg,
                                        content: assistantContent,
                                        ...(reasoningContent ? { reasoning_content: reasoningContent } : {}),
                                        ...(toolCalls.length > 0 ? { tool_calls: toolCalls } : {}),
                                    }
                                    break
                                }
                            }
                            return next
                        })
                    }

                    while (!sseDone) {
                        const { done, value } = await reader.read()
                        if (done) break

                        buffer += decoder.decode(value, { stream: true })
                        const events = buffer.split(/\r?\n\r?\n/)
                        buffer = events.pop() || ''

                        for (const event of events) {
                            const data = event
                                .split(/\r?\n/)
                                .filter((line) => line.startsWith('data:'))
                                .map((line) => line.replace(/^data:\s?/, ''))
                                .join('\n')
                                .trim()

                            if (!data) continue
                            if (data === '[DONE]') {
                                sseDone = true
                                break
                            }

                            const parsed = JSON.parse(data) as {
                                session_id?: string
                                error?: string
                                choices?: Array<{
                                    delta?: {
                                        content?: string
                                        reasoning_content?: string
                                        tool_calls?: Array<{
                                            index: number
                                            id?: string
                                            type?: string
                                            function?: { name?: string; arguments?: string }
                                        }>
                                    }
                                }>
                            }

                            if (parsed.session_id && !parsed.choices) continue
                            if (parsed.error) throw new Error(parsed.error)

                            const delta = parsed.choices?.[0]?.delta
                            if (!delta) continue

                            if (delta.content) assistantContent += delta.content
                            if (delta.reasoning_content) reasoningContent += delta.reasoning_content

                            if (delta.tool_calls) {
                                for (const tc of delta.tool_calls) {
                                    const existing = toolCallsMap.get(tc.index)
                                    if (existing) {
                                        if (tc.id) {
                                            existing.id = tc.id
                                        }
                                        if (tc.type) {
                                            existing.type = tc.type
                                        }
                                        if (tc.function?.name) {
                                            existing.name = tc.function.name
                                        }
                                        if (tc.function?.arguments) {
                                            existing.arguments += tc.function.arguments
                                        }
                                    } else {
                                        toolCallsMap.set(tc.index, {
                                            id: tc.id || '',
                                            type: tc.type || 'function',
                                            name: tc.function?.name || '',
                                            arguments: tc.function?.arguments || '',
                                        })
                                    }
                                }
                            }

                            applyStreamUpdate()
                        }
                    }

                    setStreamingTimestampState(null)
                    setAssistantResponseDone(true)
                } else {
                    const result = await response.json()

                    if (result.success && result.data?.response?.choices?.[0]?.message) {
                        const msg = result.data.response.choices[0].message
                        const assistantTimestamp = new Date().toISOString()
                        setRevealingTimestamp(assistantTimestamp)
                        const assistantMessage: ChatMessage = {
                            role: 'assistant',
                            content: msg.content || '',
                            reasoning_content: msg.reasoning_content,
                            tool_calls: msg.tool_calls,
                            timestamp: assistantTimestamp,
                        }
                        setMessages((prev) => [...prev, assistantMessage])
                        setAssistantResponseDone(true)
                    } else if (result.success === false && result.error) {
                        throw new Error(result.error)
                    } else {
                        throw new Error('Invalid response format')
                    }
                }
            } catch (error) {
                if (error instanceof DOMException && error.name === 'AbortError') {
                    aborted = true
                    return
                }
                if (error instanceof Error && (error as Error & { name?: string }).name === 'AbortError') {
                    aborted = true
                    return
                }

                const message = error instanceof Error && error.message ? error.message : '发送失败，请重试'
                onError?.(message)
                const fallbackAssistantTimestamp = new Date().toISOString()
                const activeStreamingTimestamp = streamingTimestampRef.current
                if (!activeStreamingTimestamp) {
                    setRevealingTimestamp(fallbackAssistantTimestamp)
                }

                setMessages((prev) => {
                    if (activeStreamingTimestamp) {
                        const next = [...prev]
                        for (let i = next.length - 1; i >= 0; i--) {
                            const msg = next[i]
                            if (msg.role === 'assistant' && msg.timestamp === activeStreamingTimestamp) {
                                next[i] = { ...msg, content: message }
                                return next
                            }
                        }
                    }

                    return [
                        ...prev,
                        {
                            role: 'assistant',
                            content: message,
                            timestamp: fallbackAssistantTimestamp,
                        },
                    ]
                })

                setStreamingTimestampState(null)
                setAssistantResponseDone(true)
            } finally {
                activeRequestRef.current = null
                if (aborted) {
                    resetReveal(0)
                    setStreamingTimestampState(null)
                    setAssistantResponseDone(false)
                    setRevealingTimestamp(null)
                    setAssistantVisibleSegments(0)
                    setSending(false)
                }
            }
        },
        [
            buildSendPayloadMessages,
            clearPendingOutgoingTimeout,
            onError,
            promptId,
            resetReveal,
            sending,
            sessionId,
            setMessages,
            setStreamingTimestampState,
        ]
    )

    const flushPendingMessagesRef = useRef(flushPendingMessages)

    useEffect(() => {
        flushPendingMessagesRef.current = flushPendingMessages
    }, [flushPendingMessages])

    const schedulePendingOutgoingFlush = useCallback(() => {
        const config = getReplyWaitWindowConfig()
        const delayMs = Math.max(0, Math.round(config.seconds * 1000))
        if (delayMs <= 0) {
            void flushPendingMessagesRef.current('foreground')
            return
        }

        if (config.mode === 'fixed') {
            if (pendingOutgoingTimeoutRef.current !== null) return
            pendingOutgoingTimeoutRef.current = window.setTimeout(() => {
                pendingOutgoingTimeoutRef.current = null
                void flushPendingMessagesRef.current('foreground')
            }, delayMs)
            return
        }

        clearPendingOutgoingTimeout()
        pendingOutgoingTimeoutRef.current = window.setTimeout(() => {
            pendingOutgoingTimeoutRef.current = null
            void flushPendingMessagesRef.current('foreground')
        }, delayMs)
    }, [clearPendingOutgoingTimeout])

    const sendMessage = useCallback(
        async (userMessage: ChatMessage) => {
            if (sending) return

            setMessages((prev) => [...prev, userMessage])
            pendingOutgoingMessagesRef.current.push(userMessage)
            schedulePendingOutgoingFlush()
        },
        [schedulePendingOutgoingFlush, sending, setMessages]
    )

    const abortRequest = useCallback(() => {
        activeRequestRef.current?.abort()
    }, [])

    useEffect(() => {
        const previousSessionId = lastSessionIdRef.current
        const previousPromptId = lastPromptIdRef.current
        if (previousSessionId !== sessionId) {
            clearPendingOutgoingTimeout()
            if (pendingOutgoingMessagesRef.current.length > 0) {
                void flushPendingMessagesRef.current('background', {
                    sessionId: previousSessionId,
                    promptId: previousPromptId,
                })
            }
            pendingOutgoingMessagesRef.current = []
        }
        lastSessionIdRef.current = sessionId
        lastPromptIdRef.current = promptId

        activeRequestRef.current?.abort()
        activeRequestRef.current = null
        resetReveal(0)
        setStreamingTimestampState(null)
        setAssistantResponseDone(false)
        setRevealingTimestamp(null)
        setAssistantVisibleSegments(0)
        setSending(false)
    }, [
        clearPendingOutgoingTimeout,
        promptId,
        resetReveal,
        sessionId,
        setStreamingTimestampState,
    ])

    useEffect(() => {
        return () => {
            clearPendingOutgoingTimeout()
            flushPendingMessagesOnExit()
            activeRequestRef.current?.abort()
            resetReveal(0)
        }
    }, [clearPendingOutgoingTimeout, flushPendingMessagesOnExit, resetReveal])

    useEffect(() => {
        const handlePageHide = () => {
            flushPendingMessagesOnExit()
        }

        const handleVisibilityChange = () => {
            if (document.visibilityState !== 'hidden') return
            flushPendingMessagesOnExit()
        }

        window.addEventListener('pagehide', handlePageHide)
        document.addEventListener('visibilitychange', handleVisibilityChange)
        return () => {
            window.removeEventListener('pagehide', handlePageHide)
            document.removeEventListener('visibilitychange', handleVisibilityChange)
        }
    }, [flushPendingMessagesOnExit])

    return {
        sending,
        streamingTimestamp,
        revealingTimestamp,
        assistantVisibleSegments,
        sendMessage,
        flushPendingMessages,
        abortRequest,
    }
}
