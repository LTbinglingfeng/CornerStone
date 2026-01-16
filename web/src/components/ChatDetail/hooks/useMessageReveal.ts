import { useCallback, useEffect, useRef } from 'react'
import type { ChatMessage } from '../../../types/chat'
import { ASSISTANT_BUBBLE_INTERVAL_MS, ASSISTANT_MESSAGE_SPLIT_TOKEN } from '../constants'
import { normalizeAssistantContent, splitAssistantMessageContent } from '../utils'

interface UseMessageRevealOptions {
  messages: ChatMessage[]
  sending: boolean
  streamingTimestamp: string | null
  assistantResponseDone: boolean
  setAssistantResponseDone: React.Dispatch<React.SetStateAction<boolean>>
  revealingTimestamp: string | null
  setRevealingTimestamp: React.Dispatch<React.SetStateAction<string | null>>
  assistantVisibleSegments: number
  setAssistantVisibleSegments: React.Dispatch<React.SetStateAction<number>>
  onSendingFinished: () => void
}

interface UseMessageRevealReturn {
  reset: (lastAt?: number) => void
}

export function useMessageReveal(options: UseMessageRevealOptions): UseMessageRevealReturn {
  const {
    messages,
    sending,
    streamingTimestamp,
    assistantResponseDone,
    setAssistantResponseDone,
    revealingTimestamp,
    setRevealingTimestamp,
    assistantVisibleSegments,
    setAssistantVisibleSegments,
    onSendingFinished,
  } = options

  const assistantRevealReadySegmentsRef = useRef(0)
  const assistantRevealTimeoutRef = useRef<number | null>(null)
  const assistantRevealLastAtRef = useRef(0)

  const reset = useCallback((lastAt: number = performance.now()) => {
    if (assistantRevealTimeoutRef.current !== null) {
      window.clearTimeout(assistantRevealTimeoutRef.current)
      assistantRevealTimeoutRef.current = null
    }
    assistantRevealReadySegmentsRef.current = 0
    assistantRevealLastAtRef.current = lastAt
  }, [])

  useEffect(() => {
    if (!sending) return
    if (!revealingTimestamp) return

    const currentAssistantMessage = messages.find(
      message => message.role === 'assistant' && message.timestamp === revealingTimestamp
    )
    if (!currentAssistantMessage) return

    const isStreamingAssistantMessage = streamingTimestamp === revealingTimestamp
    let assistantSegments = splitAssistantMessageContent(currentAssistantMessage.content)
    const normalizedContent = normalizeAssistantContent(currentAssistantMessage.content)
    const hasSplitToken = normalizedContent.includes(ASSISTANT_MESSAGE_SPLIT_TOKEN)
    const endsWithSplitToken = normalizedContent.trimEnd().endsWith(ASSISTANT_MESSAGE_SPLIT_TOKEN)

    const shouldHoldTrailingSegment =
      isStreamingAssistantMessage && hasSplitToken && !endsWithSplitToken && assistantSegments.length > 1
    if (shouldHoldTrailingSegment) {
      assistantSegments = assistantSegments.slice(0, -1)
    }

    assistantRevealReadySegmentsRef.current = assistantSegments.length
    if (assistantRevealTimeoutRef.current !== null) return
    if (assistantVisibleSegments >= assistantRevealReadySegmentsRef.current) return

    const now = performance.now()
    const elapsed = now - assistantRevealLastAtRef.current
    const delay = Math.max(0, ASSISTANT_BUBBLE_INTERVAL_MS - elapsed)
    assistantRevealTimeoutRef.current = window.setTimeout(() => {
      assistantRevealTimeoutRef.current = null
      assistantRevealLastAtRef.current = performance.now()
      setAssistantVisibleSegments(prev => Math.min(prev + 1, assistantRevealReadySegmentsRef.current))
    }, delay)
  }, [assistantVisibleSegments, messages, revealingTimestamp, sending, setAssistantVisibleSegments, streamingTimestamp])

  useEffect(() => {
    if (!sending) return
    if (!assistantResponseDone) return
    if (!revealingTimestamp) return

    if (assistantVisibleSegments < assistantRevealReadySegmentsRef.current) return
    if (assistantRevealTimeoutRef.current !== null) {
      window.clearTimeout(assistantRevealTimeoutRef.current)
      assistantRevealTimeoutRef.current = null
    }
    assistantRevealReadySegmentsRef.current = 0
    setAssistantResponseDone(false)
    setRevealingTimestamp(null)
    setAssistantVisibleSegments(0)
    onSendingFinished()
  }, [
    assistantResponseDone,
    assistantVisibleSegments,
    onSendingFinished,
    revealingTimestamp,
    sending,
    setAssistantResponseDone,
    setAssistantVisibleSegments,
    setRevealingTimestamp,
  ])

  return { reset }
}
