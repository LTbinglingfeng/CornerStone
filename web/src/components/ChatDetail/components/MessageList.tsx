import { useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import type { ChatMessage, Prompt, UserInfo } from '../../../types/chat'
import type { ActiveRedPacketState, DisplayItem } from '../types'
import type { PacketStep } from '../RedPacket'
import { PatBanner, RedPacketBubble, RedPacketReceivedBanner, derivePacketKeys } from '../RedPacket'
import { MessageAvatar } from './MessageAvatar'
import { MessageBubble } from './MessageBubble'

interface MessageListProps {
    sessionId: string
    loading: boolean
    messages: ChatMessage[]
    userInfo: UserInfo | null
    prompt: Prompt | null
    listRef: React.RefObject<HTMLDivElement | null>
    displayItems: DisplayItem[]
    openedRedPacketKeys: Set<string>
    getImageUrl: (imagePath: string) => string
    userAvatarSrc: string | null
    assistantAvatarSrc: string | null
    userPlaceholder: string
    assistantPlaceholder: string
    userDisplayName: string
    assistantDisplayName: string
    onPatAssistant: () => void
    onOpenRedPacket: (active: ActiveRedPacketState, initialStep: PacketStep) => void
    onContextMenu: (e: React.MouseEvent, item: DisplayItem) => void
    onPointerDown: (e: React.PointerEvent, item: DisplayItem) => void
    onPointerMove: (e: React.PointerEvent) => void
    onPointerUp: () => void
    onPointerCancel: () => void
    onPointerLeave: () => void
}

export const MessageList: React.FC<MessageListProps> = ({
    sessionId,
    loading,
    messages,
    userInfo,
    prompt,
    listRef,
    displayItems,
    openedRedPacketKeys,
    getImageUrl,
    userAvatarSrc,
    assistantAvatarSrc,
    userPlaceholder,
    assistantPlaceholder,
    userDisplayName,
    assistantDisplayName,
    onPatAssistant,
    onOpenRedPacket,
    onContextMenu,
    onPointerDown,
    onPointerMove,
    onPointerUp,
    onPointerCancel,
    onPointerLeave,
}) => {
    const animatedBubbleKeysRef = useRef<Set<string>>(new Set())
    const bubbleKeysSeededRef = useRef(false)

    useEffect(() => {
        animatedBubbleKeysRef.current.clear()
        bubbleKeysSeededRef.current = false
    }, [sessionId])

    useEffect(() => {
        if (loading) return
        if (!listRef.current) return
        listRef.current.scrollTop = listRef.current.scrollHeight
    }, [displayItems, listRef, loading])

    useEffect(() => {
        if (loading) return
        const list = listRef.current
        if (!list) return

        if (!bubbleKeysSeededRef.current) {
            displayItems.forEach((item) => animatedBubbleKeysRef.current.add(item.key))
            bubbleKeysSeededRef.current = true
            return
        }

        const escapeForSelector = (value: string) => {
            if (typeof window !== 'undefined' && window.CSS && typeof window.CSS.escape === 'function') {
                return window.CSS.escape(value)
            }
            return value.replace(/[\"\\\\]/g, '\\\\$&')
        }

        displayItems.forEach((item) => {
            if (animatedBubbleKeysRef.current.has(item.key)) return
            animatedBubbleKeysRef.current.add(item.key)

            if (item.type !== 'text' && item.type !== 'red-packet') return
            const target = list.querySelector<HTMLElement>(`[data-bubble-key="${escapeForSelector(item.key)}"]`)
            if (!target) return

            gsap.killTweensOf(target)
            gsap.fromTo(
                target,
                { opacity: 0, y: 12, scale: 0.97 },
                { opacity: 1, y: 0, scale: 1, duration: 0.32, ease: 'power2.out', clearProps: 'transform,opacity' }
            )
        })
    }, [displayItems, listRef, loading])

    return (
        <div className="message-list" ref={listRef}>
            {loading ? (
                <div className="empty-messages">加载中...</div>
            ) : displayItems.length === 0 ? (
                <div className="empty-messages">开始新的对话</div>
            ) : (
                displayItems.map((item) => {
                    const isRedPacket = item.type === 'red-packet'
                    const isRedPacketReceivedBanner = item.type === 'red-packet-received-banner'
                    const isPatBanner = item.type === 'pat-banner'
                    const isRecallBanner = item.type === 'recall-banner'

                    if (isRedPacketReceivedBanner) {
                        return (
                            <div key={item.key} className="message-item pat-banner-item">
                                <RedPacketReceivedBanner
                                    toolCall={item.toolCall}
                                    messages={messages}
                                    userInfo={userInfo}
                                    prompt={prompt}
                                />
                            </div>
                        )
                    }

                    if (isPatBanner) {
                        return (
                            <div key={item.key} className="message-item pat-banner-item">
                                <PatBanner toolCall={item.toolCall} prompt={prompt} />
                            </div>
                        )
                    }

                    if (isRecallBanner) {
                        return (
                            <div key={item.key} className="message-item pat-banner-item">
                                <div className="pat-banner">你撤回了一条消息</div>
                            </div>
                        )
                    }

                    const role: 'user' | 'assistant' = item.role === 'user' ? 'user' : 'assistant'
                    const leftAvatar =
                        role === 'assistant' ? (
                            <MessageAvatar
                                role="assistant"
                                avatarSrc={assistantAvatarSrc}
                                placeholder={assistantPlaceholder}
                                onDoubleClick={(e) => {
                                    e.stopPropagation()
                                    onPatAssistant()
                                }}
                                title="双击拍一拍"
                            />
                        ) : null
                    const rightAvatar =
                        role === 'user' ? (
                            <MessageAvatar role="user" avatarSrc={userAvatarSrc} placeholder={userPlaceholder} />
                        ) : null

                    const content = isRedPacket ? (
                        (() => {
                            const { primaryKey, legacyKey } = derivePacketKeys(item.toolCall, item.key)
                            const opened = openedRedPacketKeys.has(primaryKey) || openedRedPacketKeys.has(legacyKey)
                            const packetKey = openedRedPacketKeys.has(legacyKey) ? legacyKey : primaryKey
                            const senderName =
                                role === 'user' ? userDisplayName || '我' : assistantDisplayName || 'AI Assistant'
                            const senderAvatarSrc = role === 'user' ? userAvatarSrc : assistantAvatarSrc
                            const initialStep: PacketStep = role === 'user' || opened ? 'opened' : 'idle'
                            return (
                                <RedPacketBubble
                                    toolCall={item.toolCall}
                                    rawKey={item.key}
                                    role={role}
                                    opened={opened}
                                    senderName={senderName}
                                    senderAvatarSrc={senderAvatarSrc}
                                    onClick={(params) => {
                                        onOpenRedPacket(
                                            {
                                                params,
                                                packetKey,
                                                senderRole: role,
                                                senderName,
                                                senderAvatarSrc: senderAvatarSrc || null,
                                            },
                                            initialStep
                                        )
                                    }}
                                />
                            )
                        })()
                    ) : (
                        <MessageBubble
                            item={item}
                            getImageUrl={getImageUrl}
                            onContextMenu={onContextMenu}
                            onPointerDown={onPointerDown}
                            onPointerMove={onPointerMove}
                            onPointerUp={onPointerUp}
                            onPointerCancel={onPointerCancel}
                            onPointerLeave={onPointerLeave}
                        />
                    )

                    return (
                        <div
                            key={item.key}
                            className={`message-item ${item.role} ${isRedPacket ? 'red-packet-item' : ''}`}
                        >
                            {leftAvatar}
                            {content}
                            {rightAvatar}
                        </div>
                    )
                })
            )}
        </div>
    )
}
