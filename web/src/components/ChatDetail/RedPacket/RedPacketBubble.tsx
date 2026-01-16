import type { ToolCall } from '../../../types/chat'
import type { RedPacketParams } from '../../../types/chat'

interface RedPacketBubbleProps {
    toolCall: ToolCall
    rawKey: string
    role: 'user' | 'assistant'
    opened: boolean
    senderName: string
    senderAvatarSrc: string | null
    onClick: (params: RedPacketParams) => void
}

export const RedPacketBubble: React.FC<RedPacketBubbleProps> = ({ toolCall, rawKey, role, opened, onClick }) => {
    if (toolCall.function.name !== 'send_red_packet') return null

    try {
        const params: RedPacketParams = JSON.parse(toolCall.function.arguments)
        const shouldTreatAsOpened = role === 'user' || opened
        return (
            <div
                className="red-packet-bubble"
                data-bubble-key={rawKey}
                onClick={() => {
                    onClick(params)
                }}
            >
                <div className="rp-content">
                    <div className="rp-icon-wrapper">
                        <svg viewBox="0 0 40 40" className="rp-icon">
                            <path
                                d="M35.5,14.5c0-1.6-0.8-3-2.1-3.9l-10-6.7c-2.1-1.4-4.8-1.4-6.9,0l-10,6.7C5.3,11.5,4.5,12.9,4.5,14.5v16c0,2.5,2,4.5,4.5,4.5h22c2.5,0,4.5-2,4.5-4.5V14.5z M20,9.5l8.9,6L20,21.4L11.1,15.5L20,9.5z M9,31v-8.8l7.2,4.8L9,31z M20,25.6l-2.4-1.6l-2.4,3.2c-0.9,1.2-2.3,1.9-3.7,1.9h-1.3v2H20V25.6z M31,31h-9.8v-5.5h1.3c1.5,0,2.8-0.7,3.7-1.9l-2.4-3.2l-2.4,1.6l3.8,2.5L31,22.2V31z"
                                fill="var(--red-packet-header-text)"
                            />
                        </svg>
                    </div>
                    <div className="rp-text">
                        <div className="rp-title">{params.message || '恭喜发财，大吉大利'}</div>
                        <div className="rp-status">
                            {role === 'user' ? '查看红包' : shouldTreatAsOpened ? '已领取' : '领取红包'}
                        </div>
                    </div>
                </div>
                <div className="rp-footer">微信红包</div>
            </div>
        )
    } catch {
        return null
    }
}
