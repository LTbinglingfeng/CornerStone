import { useRef, useCallback } from 'react'
import type { ChatSession } from '../types/chat'
import { useT } from '../contexts/I18nContext'
import { formatTime } from '../utils/time'
import './ChatItem.css'

interface ChatItemProps {
    session: ChatSession
    onClick: (id: string) => void
    onLongPress?: (id: string, position: { x: number; y: number }) => void
}

function getAvatarInfo(title: string): { text: string; color: string } {
    const colors = ['#4a90d9', '#7ed321', '#bd10e0', '#f5a623', '#50e3c2', '#9013fe', '#417505', '#2b5797']
    const firstChar = title.charAt(0) || '?'
    const colorIndex = firstChar.charCodeAt(0) % colors.length
    return { text: firstChar, color: colors[colorIndex] }
}

const ChatItem: React.FC<ChatItemProps> = ({ session, onClick, onLongPress }) => {
    const { t } = useT()
    const avatar = getAvatarInfo(session.title)
    const longPressTimer = useRef<number | null>(null)
    const isLongPress = useRef(false)

    const handleTouchStart = useCallback(
        (e: React.TouchEvent) => {
            isLongPress.current = false
            const touch = e.touches[0]
            const position = { x: touch.clientX, y: touch.clientY }

            longPressTimer.current = window.setTimeout(() => {
                isLongPress.current = true
                onLongPress?.(session.id, position)
            }, 500)
        },
        [session.id, onLongPress]
    )

    const handleTouchEnd = useCallback(() => {
        if (longPressTimer.current) {
            clearTimeout(longPressTimer.current)
            longPressTimer.current = null
        }
    }, [])

    const handleTouchMove = useCallback(() => {
        if (longPressTimer.current) {
            clearTimeout(longPressTimer.current)
            longPressTimer.current = null
        }
    }, [])

    const handleClick = useCallback(() => {
        if (!isLongPress.current) {
            onClick(session.id)
        }
    }, [onClick, session.id])

    const handleContextMenu = useCallback(
        (e: React.MouseEvent) => {
            e.preventDefault()
            onLongPress?.(session.id, { x: e.clientX, y: e.clientY })
        },
        [session.id, onLongPress]
    )

    return (
        <div
            className="chat-item"
            onClick={handleClick}
            onTouchStart={handleTouchStart}
            onTouchEnd={handleTouchEnd}
            onTouchMove={handleTouchMove}
            onContextMenu={handleContextMenu}
        >
            <div className="avatar-container">
                <div className="avatar" style={{ backgroundColor: avatar.color }}>
                    {avatar.text}
                </div>
            </div>
            <div className="chat-content">
                <div className="chat-header">
                    <span className="chat-name">{session.title}</span>
                    <span className="chat-time">{formatTime(session.updated_at)}</span>
                </div>
                <div className="chat-message">{t('chat.clickToView')}</div>
            </div>
        </div>
    )
}

export default ChatItem
