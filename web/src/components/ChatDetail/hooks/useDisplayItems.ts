import { useMemo } from 'react'
import type { ChatMessage } from '../../../types/chat'
import { ASSISTANT_MESSAGE_SPLIT_TOKEN } from '../constants'
import type { DisplayItem } from '../types'
import { isRecalledMessage, normalizeAssistantContent, splitAssistantMessageContent } from '../utils'

interface UseDisplayItemsOptions {
  messages: ChatMessage[]
  sending: boolean
  streamingTimestamp: string | null
  revealingTimestamp: string | null
  visibleSegments: number
}

export function useDisplayItems(options: UseDisplayItemsOptions): DisplayItem[] {
  const { messages, sending, streamingTimestamp, revealingTimestamp, visibleSegments } = options

  return useMemo(() => {
    const items: DisplayItem[] = []
    messages.forEach((message, index) => {
      const hasImages = !!(message.image_paths && message.image_paths.length > 0)
      const toolCalls = message.tool_calls || []
      const supportedCalls = toolCalls.filter(
        tc =>
          tc.function.name === 'send_red_packet' ||
          tc.function.name === 'send_pat' ||
          tc.function.name === 'red_packet_received'
      )

      const isAssistant = message.role === 'assistant'
      const isStreamingAssistantMessage =
        isAssistant && sending && streamingTimestamp !== null && message.timestamp === streamingTimestamp

      let assistantSegments = isAssistant ? splitAssistantMessageContent(message.content) : []
      const normalizedContent = isAssistant ? normalizeAssistantContent(message.content) : ''
      const hasSplitToken = isAssistant && normalizedContent.includes(ASSISTANT_MESSAGE_SPLIT_TOKEN)
      const endsWithSplitToken = isAssistant && normalizedContent.trimEnd().endsWith(ASSISTANT_MESSAGE_SPLIT_TOKEN)

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
      const hasContent = hasText || hasImages

      if (hasContent) {
        if (isAssistant) {
          if (assistantSegments.length > 0) {
            assistantSegments.forEach((segment, segmentIndex) => {
              const segmentMessage: ChatMessage = {
                ...message,
                content: segment,
                ...(segmentIndex === 0 ? {} : { image_paths: undefined }),
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
              message: { ...message, content: '' },
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
    })
    return items
  }, [messages, sending, streamingTimestamp, revealingTimestamp, visibleSegments])
}

