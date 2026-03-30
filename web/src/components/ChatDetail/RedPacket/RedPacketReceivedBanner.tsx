import type { ChatMessage, Prompt, ToolCall, UserInfo } from '../../../types/chat'
import { useT } from '../../../contexts/I18nContext'
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
    const { t } = useT()
    const redPacketWordToken = '__RED_PACKET_WORD__'
    if (toolCall.function.name !== 'red_packet_received') return null

    let receiverName = userInfo?.username?.trim() || t('chat.defaultTargetName')
    let senderName = prompt?.name?.trim() || t('chat.defaultAIName')
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
                userInfo?.username?.trim() || t('chat.defaultTargetName'),
                prompt?.name?.trim() || t('chat.defaultAIName')
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

    const redPacketWord = t('redPacket.redPacketWord')
    const receivedBannerText = t('redPacket.receivedBanner', {
        receiverName,
        senderName,
        redPacket: redPacketWordToken,
    })
    const [bannerPrefix, bannerSuffix = ''] = receivedBannerText.split(redPacketWordToken)
    const hasRedPacketWordToken = receivedBannerText.includes(redPacketWordToken)

    return (
        <div className="red-packet-received-banner">
            <svg className="red-packet-received-banner-icon" viewBox="0 0 24 24" aria-hidden="true">
                <rect x="3" y="3" width="18" height="18" rx="4" fill="#e8554e" />
                <circle cx="12" cy="12" r="3.6" fill="#f5d27a" />
                <circle cx="12" cy="12" r="1.4" fill="#d4a94a" />
            </svg>
            <span className="red-packet-received-banner-text">
                {hasRedPacketWordToken ? (
                    <>
                        {bannerPrefix}
                        <span className="red-packet-received-banner-highlight">{redPacketWord}</span>
                        {bannerSuffix}
                    </>
                ) : (
                    receivedBannerText
                )}
            </span>
        </div>
    )
}
