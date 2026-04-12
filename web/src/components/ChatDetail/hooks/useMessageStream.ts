import { useCallback, useEffect, useRef, useState } from 'react'
import type { ChatMessage } from '../../../types/chat'
import { translate } from '../../../i18n'
import { sendMessage as sendMessageApi, sendMessageBeacon as sendMessageBeaconApi } from '../../../services/api'
import { getReplyWaitWindowConfig } from '../../../utils/replyWaitWindow'
import { useMessageReveal } from './useMessageReveal'

type FlushMode = 'foreground' | 'background'

interface UseMessageStreamOptions {
    sessionId: string
    promptId?: string
    messages: ChatMessage[]
    setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>
    assistantMessageSplitToken: string
    onError?: (error: string) => void
}

interface UseMessageStreamReturn {
    sending: boolean
    streamingTimestamp: string | null
    revealingTimestamp: string | null
    assistantVisibleSegments: number
    sendMessage: (userMessage: ChatMessage) => Promise<void>
    flushPendingMessages: (mode: FlushMode, override?: { sessionId: string; promptId?: string }) => Promise<void>
    regenerateLastMessage: () => Promise<void>
    abortRequest: () => void
}

export function useMessageStream(options: UseMessageStreamOptions): UseMessageStreamReturn {
    const { sessionId, promptId, messages, setMessages, assistantMessageSplitToken, onError } = options

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
        assistantMessageSplitToken,
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
            outgoingMessages.map(({ role, content, tool_call_id, image_paths, tool_calls }) => ({
                role,
                content,
                ...(tool_call_id ? { tool_call_id } : {}),
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
        async (mode: FlushMode, override?: { sessionId: string; promptId?: string; regenerate?: boolean }) => {
            const isRegenerate = override?.regenerate === true
            if (mode === 'foreground' && sending) return

            const pendingMessages = pendingOutgoingMessagesRef.current
            if (!isRegenerate && pendingMessages.length === 0) return

            // regenerate 和普通发送都需清空 pending 队列与定时器
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

                const response = await sendMessageApi(
                    targetSessionId,
                    isRegenerate ? [] : buildSendPayloadMessages(pendingMessages),
                    {
                        promptId: targetPromptId,
                        signal: abortController.signal,
                        regenerate: isRegenerate || undefined,
                    }
                )

                const contentType = response.headers.get('Content-Type') || ''
                const isStreaming = contentType.includes('text/event-stream')

                if (isStreaming) {
                    const reader = response.body?.getReader()
                    const decoder = new TextDecoder()

                    if (!reader) {
                        throw new Error('No response body')
                    }

                    let finalAssistantTimestamp: string | null = null
                    let buffer = ''
                    let sseDone = false

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
                                type?: string
                                timestamp?: string
                                tts_audio_paths?: string[]
                                message?: ChatMessage
                                choices?: Array<{ delta?: unknown }>
                            }

                            if (
                                parsed.session_id &&
                                !parsed.type &&
                                !parsed.message &&
                                !parsed.choices &&
                                !parsed.tts_audio_paths
                            )
                                continue
                            if (parsed.error) throw new Error(parsed.error)

                            if (parsed.type === 'message' && parsed.message) {
                                const message = parsed.message
                                setMessages((prev) => [...prev, message])
                                if (
                                    message.role === 'assistant' &&
                                    (!message.tool_calls || message.tool_calls.length === 0)
                                ) {
                                    finalAssistantTimestamp = message.timestamp
                                    setRevealingTimestamp(message.timestamp)
                                }
                                continue
                            }

                            if (parsed.type === 'tts_audio' && parsed.tts_audio_paths) {
                                const ttsPaths = parsed.tts_audio_paths
                                const timestamp =
                                    typeof parsed.timestamp === 'string' && parsed.timestamp.trim() !== ''
                                        ? parsed.timestamp
                                        : finalAssistantTimestamp
                                if (!timestamp) continue
                                setMessages((prev) => {
                                    const next = [...prev]
                                    for (let i = next.length - 1; i >= 0; i--) {
                                        const msg = next[i]
                                        if (msg.role === 'assistant' && msg.timestamp === timestamp) {
                                            next[i] = { ...msg, tts_audio_paths: ttsPaths }
                                            break
                                        }
                                    }
                                    return next
                                })
                                continue
                            }

                            if (parsed.tts_audio_paths) {
                                const ttsPaths = parsed.tts_audio_paths
                                const timestamp = finalAssistantTimestamp
                                if (!timestamp) continue
                                setMessages((prev) => {
                                    const next = [...prev]
                                    for (let i = next.length - 1; i >= 0; i--) {
                                        const msg = next[i]
                                        if (msg.role === 'assistant' && msg.timestamp === timestamp) {
                                            next[i] = { ...msg, tts_audio_paths: ttsPaths }
                                            break
                                        }
                                    }
                                    return next
                                })
                                continue
                            }
                        }
                    }

                    setStreamingTimestampState(null)
                    setAssistantResponseDone(true)
                } else {
                    const result = await response.json()

                    if (result.success && Array.isArray(result.data?.messages) && result.data.messages.length > 0) {
                        const newMessages = result.data.messages as ChatMessage[]
                        setMessages((prev) => [...prev, ...newMessages])
                        const finalAssistant = [...newMessages]
                            .reverse()
                            .find(
                                (message) =>
                                    message.role === 'assistant' &&
                                    (!message.tool_calls || message.tool_calls.length === 0)
                            )
                        if (finalAssistant) {
                            setRevealingTimestamp(finalAssistant.timestamp)
                        }
                        setAssistantResponseDone(true)
                    } else if (result.success && result.data?.response?.choices?.[0]?.message) {
                        const msg = result.data.response.choices[0].message
                        const assistantTimestamp = new Date().toISOString()
                        setRevealingTimestamp(assistantTimestamp)
                        const assistantMessage: ChatMessage = {
                            role: 'assistant',
                            content: msg.content || '',
                            reasoning_content: msg.reasoning_content,
                            tool_calls: msg.tool_calls,
                            tts_audio_paths: msg.tts_audio_paths,
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

                const message =
                    error instanceof Error && error.message ? error.message : translate('common.operationFailed')
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

    const regenerateLastMessage = useCallback(async () => {
        if (sending) return

        // Hook 层防御：仅当会话尾部存在上一轮 AI 响应（尾部不是 user）时才允许重生
        const tailMessage = messages[messages.length - 1]
        if (!tailMessage || tailMessage.role === 'user') return

        // 有待发送的用户消息时禁止重生（尾部实际已不是 assistant）
        if (pendingOutgoingMessagesRef.current.length > 0) return

        // 清除待发送定时器，防止 regenerate 期间 timeout 触发后 pending 卡住
        clearPendingOutgoingTimeout()

        // 从本地状态移除最后一条 user 之后的整段尾部响应（assistant/tool 批次）
        setMessages((prev) => {
            const n = prev.length
            if (n === 0 || prev[n - 1].role === 'user') return prev

            let cutIndex = n
            while (cutIndex > 0 && prev[cutIndex - 1].role !== 'user') {
                cutIndex--
            }
            return prev.slice(0, cutIndex)
        })

        await flushPendingMessages('foreground', { sessionId, promptId: promptId, regenerate: true })
    }, [clearPendingOutgoingTimeout, flushPendingMessages, messages, promptId, sending, sessionId, setMessages])

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
    }, [clearPendingOutgoingTimeout, promptId, resetReveal, sessionId, setStreamingTimestampState])

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
        regenerateLastMessage,
        abortRequest,
    }
}
