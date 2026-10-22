import { useEffect, useLayoutEffect, useRef } from 'react'
import { motion } from 'motion/react'
import type { ChatMessage, Prompt, UserInfo } from '../../../types/chat'
import { useT } from '../../../contexts/I18nContext'
import type { ActiveRedPacketState, DisplayItem } from '../types'
import type { PacketStep } from '../RedPacket'
import { PatBanner, RedPacketBubble, RedPacketReceivedBanner, derivePacketKeys } from '../RedPacket'
import { bubbleVariants } from '../../../utils/motion'
import { getManagementCopy } from '../../../i18n/archive'
import { EmptyState } from '../../ui'
import { MessageAvatar } from './MessageAvatar'
import { MessageBubble } from './MessageBubble'
import { ToolCallCard } from './ToolCallCard'

interface MessageListProps {
    sessionId: string
    loading: boolean
    hasMoreBefore: boolean
    loadingOlder: boolean
    onLoadOlder: () => Promise<void>
    messages: ChatMessage[]
    userInfo: UserInfo | null
    prompt: Prompt | null
    listRef: React.RefObject<HTMLDivElement | null>
    displayItems: DisplayItem[]
    openedRedPacketKeys: Set<string>
    getImageUrl: (imagePath: string) => string
    userAvatarSrc: string | null
    assistantAvatarSrc: string | null
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
    hasMoreBefore,
    loadingOlder,
    onLoadOlder,
    messages,
    userInfo,
    prompt,
    listRef,
    displayItems,
    openedRedPacketKeys,
    getImageUrl,
    userAvatarSrc,
    assistantAvatarSrc,
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
    const { t, locale } = useT()
    const copy = getManagementCopy(locale).conversation
    const animatedBubbleKeysRef = useRef<Set<string>>(new Set())
    const bubbleKeysSeededRef = useRef(false)
    const shouldScrollToBottomRef = useRef(true)
    const isAtBottomRef = useRef(true)

    useEffect(() => {
        animatedBubbleKeysRef.current.clear()
        bubbleKeysSeededRef.current = false
        shouldScrollToBottomRef.current = true
        isAtBottomRef.current = true
    }, [sessionId])

    useLayoutEffect(() => {
        if (loading) return
        if (!listRef.current) return
        if (!shouldScrollToBottomRef.current && !isAtBottomRef.current) return
        listRef.current.scrollTop = listRef.current.scrollHeight
        shouldScrollToBottomRef.current = false
    }, [displayItems, listRef, loading])

    useEffect(() => {
        if (loading) return

        if (!bubbleKeysSeededRef.current) {
            displayItems.forEach((item) => animatedBubbleKeysRef.current.add(item.key))
            bubbleKeysSeededRef.current = true
            return
        }
    }, [displayItems, listRef, loading])

    useEffect(() => {
        if (loading || !bubbleKeysSeededRef.current) return
        displayItems.forEach((item) => animatedBubbleKeysRef.current.add(item.key))
    }, [displayItems, loading])

    const handleScroll = () => {
        const list = listRef.current
        if (!list) return

        const distanceToBottom = list.scrollHeight - list.scrollTop - list.clientHeight
        isAtBottomRef.current = distanceToBottom <= 80

        if (!hasMoreBefore || loadingOlder) return
        if (list.scrollTop > 120) return
        void onLoadOlder()
    }

    const shouldAnimateBubble = (item: DisplayItem) => {
        if (!bubbleKeysSeededRef.current) return false
        if (item.type !== 'text' && item.type !== 'red-packet' && item.type !== 'tool-call') return false
        return !animatedBubbleKeysRef.current.has(item.key)
    }

    return (
        <div className="message-list" ref={listRef} onScroll={handleScroll}>
            {loading ? (
                <div className="empty-messages" role="status">
                    <span className="ui-spinner" aria-hidden="true" />
                    <span className="ui-visually-hidden">{t('common.loading')}</span>
                </div>
            ) : displayItems.length === 0 ? (
                <div className="empty-messages">
                    <EmptyState icon="message" title={copy.emptyMessages} description={copy.emptyMessagesHint} />
                </div>
            ) : (
                <>
                    {hasMoreBefore && (
                        <div className="message-load-more">
                            {loadingOlder && <span className="ui-spinner" aria-hidden="true" />}
                            {loadingOlder ? t('common.loading') : t('chat.loadMore')}
                        </div>
                    )}
                    {displayItems.map((item, index) => {
                        const isRedPacket = item.type === 'red-packet'
                        const isRedPacketReceivedBanner = item.type === 'red-packet-received-banner'
                        const isPatBanner = item.type === 'pat-banner'
                        const isNoReplyBanner = item.type === 'no-reply-banner'
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

                        if (item.type === 'attachment-intent-banner') {
                            return (
                                <div key={item.key} className="message-item pat-banner-item">
                                    <div className="pat-banner">{t('chat.attachmentIntentBanner')}</div>
                                </div>
                            )
                        }

                        if (isNoReplyBanner) {
                            return (
                                <div key={item.key} className="message-item pat-banner-item">
                                    <div className="pat-banner">{t('chat.noReplyBanner')}</div>
                                </div>
                            )
                        }

                        if (isRecallBanner) {
                            return (
                                <div key={item.key} className="message-item pat-banner-item">
                                    <div className="pat-banner">{t('chat.recalledBanner')}</div>
                                </div>
                            )
                        }

                        const role: 'user' | 'assistant' = item.role === 'user' ? 'user' : 'assistant'
                        const previous = index > 0 ? displayItems[index - 1] : null
                        // Consecutive bubbles from the same sender read as one turn: one avatar, tighter spacing.
                        const grouped =
                            !!previous &&
                            (previous.type === 'text' ||
                                previous.type === 'red-packet' ||
                                previous.type === 'tool-call') &&
                            (previous.role === 'user' ? 'user' : 'assistant') === role
                        const leftAvatar =
                            role === 'assistant' && !grouped ? (
                                <MessageAvatar
                                    role="assistant"
                                    avatarSrc={assistantAvatarSrc}
                                    placeholder={assistantPlaceholder}
                                    onDoubleClick={(e) => {
                                        e.stopPropagation()
                                        onPatAssistant()
                                    }}
                                    title={t('chat.doubleTapPoke')}
                                />
                            ) : role === 'assistant' ? (
                                <span className="message-avatar-spacer" aria-hidden="true" />
                            ) : null

                        const content =
                            item.type === 'tool-call' ? (
                                <ToolCallCard
                                    toolCall={item.toolCall}
                                    result={item.result}
                                    isRunning={item.isRunning}
                                />
                            ) : isRedPacket ? (
                                (() => {
                                    const { primaryKey, legacyKey } = derivePacketKeys(item.toolCall, item.key)
                                    const opened =
                                        openedRedPacketKeys.has(primaryKey) || openedRedPacketKeys.has(legacyKey)
                                    const packetKey = openedRedPacketKeys.has(legacyKey) ? legacyKey : primaryKey
                                    const senderName =
                                        role === 'user'
                                            ? userDisplayName || t('chat.defaultUserName')
                                            : assistantDisplayName || t('chat.defaultAIName')
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

                        const className = `message-item ${role}${isRedPacket ? ' red-packet-item' : ''}${item.type === 'tool-call' ? ' tool-call-item' : ''}${grouped ? ' is-grouped' : ''}`
                        const animateBubble = shouldAnimateBubble(item)

                        return (
                            <motion.div
                                key={item.key}
                                className={className}
                                initial={animateBubble ? 'hidden' : false}
                                animate="visible"
                                variants={bubbleVariants}
                            >
                                {leftAvatar}
                                {content}
                            </motion.div>
                        )
                    })}
                </>
            )}
        </div>
    )
}
