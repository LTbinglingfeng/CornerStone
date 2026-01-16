import type { ChatMessage, Prompt, ToolCall, UserInfo } from '../../../types/chat'
import { inferRedPacketParties } from './utils'

interface RedPacketReceivedBannerProps {
  toolCall: ToolCall
  messages: ChatMessage[]
  userInfo: UserInfo | null
  prompt: Prompt | null
}

export const RedPacketReceivedBanner: React.FC<RedPacketReceivedBannerProps> = ({
  toolCall,
  messages,
  userInfo,
  prompt,
}) => {
  if (toolCall.function.name !== 'red_packet_received') return null

  let receiverName = userInfo?.username?.trim() || '你'
  let senderName = prompt?.name?.trim() || 'AI Assistant'
  let packetKey = ''
  let inferredSenderRole: null | 'user' | 'assistant' = null

  try {
    const args = JSON.parse(toolCall.function.arguments || '{}') as {
      packet_key?: unknown
      receiver_name?: unknown
      sender_name?: unknown
    }
    if (typeof args.packet_key === 'string' && args.packet_key.trim() !== '') {
      packetKey = args.packet_key.trim()
    }

    if (packetKey) {
      const inferred = inferRedPacketParties(
        messages,
        packetKey,
        userInfo?.username?.trim() || '你',
        prompt?.name?.trim() || 'AI Assistant'
      )
      if (inferred) {
        receiverName = inferred.receiverName
        senderName = inferred.senderName
        inferredSenderRole = inferred.senderRole
      }
    }

    const shouldTrustToolNames = inferredSenderRole !== 'user'
    if (shouldTrustToolNames) {
      if (typeof args.receiver_name === 'string' && args.receiver_name.trim() !== '') {
        receiverName = args.receiver_name.trim()
      }
      if (typeof args.sender_name === 'string' && args.sender_name.trim() !== '') {
        senderName = args.sender_name.trim()
      }
    }
  } catch {
    // ignore invalid payload
  }

  return (
    <div className="red-packet-received-banner">
      <svg className="red-packet-received-banner-icon" viewBox="0 0 24 24" aria-hidden="true">
        <rect x="3" y="3" width="18" height="18" rx="4" fill="#e8554e" />
        <circle cx="12" cy="12" r="3.6" fill="#f5d27a" />
        <circle cx="12" cy="12" r="1.4" fill="#d4a94a" />
      </svg>
      <span className="red-packet-received-banner-text">
        {receiverName}领取了{senderName}的<span className="red-packet-received-banner-highlight">红包</span>
      </span>
    </div>
  )
}

