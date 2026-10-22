import type { ChatMessage, RedPacketParams, ToolCall } from '../../types/chat'

export interface ChatDetailProps {
    sessionId: string
    promptId?: string
    onBack: () => void
    onSwitchSession?: (sessionId: string, promptId?: string) => void
}

export type DisplayItem =
    | { key: string; role: string; type: 'text'; message: ChatMessage; messageIndex: number }
    | { key: string; role: string; type: 'red-packet'; message: ChatMessage; toolCall: ToolCall; messageIndex: number }
    | {
          key: string
          role: string
          type: 'red-packet-received-banner'
          message: ChatMessage
          toolCall: ToolCall
          messageIndex: number
      }
    | { key: string; role: string; type: 'pat-banner'; message: ChatMessage; toolCall: ToolCall; messageIndex: number }
    | {
          key: string
          role: string
          type: 'no-reply-banner' | 'attachment-intent-banner'
          message: ChatMessage
          toolCall: ToolCall
          messageIndex: number
      }
    | { key: string; role: string; type: 'recall-banner'; message: ChatMessage; messageIndex: number }
    | {
          key: string
          role: string
          type: 'tool-call'
          message: ChatMessage
          toolCall: ToolCall
          /** Raw content of the matching role=tool message; undefined while the call has no result yet. */
          result?: string
          messageIndex: number
      }

export type QuoteDraft = {
    line: string
}

export type MessageMenuState = {
    position: { x: number; y: number }
    messageIndex: number
    message: ChatMessage
}

export type MessageEditState = {
    messageIndex: number
    quoteLine: string | null
    text: string
}

export type SelectTextState = {
    text: string
}

export type ActiveRedPacketState = {
    params: RedPacketParams
    packetKey: string
    senderRole: 'user' | 'assistant'
    senderName: string
    senderAvatarSrc: string | null
}
