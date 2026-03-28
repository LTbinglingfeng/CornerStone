import { useEffect, useRef, useState } from 'react'
import type { DisplayItem } from '../types'
import { parseQuotedMessageContent } from '../utils'
import { MessageImages } from './MessageImages'
import { getTTSAudioUrl } from '../../../services/api'

interface MessageBubbleProps {
    item: DisplayItem
    getImageUrl: (imagePath: string) => string
    onContextMenu: (e: React.MouseEvent, item: DisplayItem) => void
    onPointerDown: (e: React.PointerEvent, item: DisplayItem) => void
    onPointerMove: (e: React.PointerEvent) => void
    onPointerUp: () => void
    onPointerCancel: () => void
    onPointerLeave: () => void
}

export const MessageBubble: React.FC<MessageBubbleProps> = ({
    item,
    getImageUrl,
    onContextMenu,
    onPointerDown,
    onPointerMove,
    onPointerUp,
    onPointerCancel,
    onPointerLeave,
}) => {
    if (item.type !== 'text') return null

    const parsedQuote = parseQuotedMessageContent(item.message.content)
    const quoteLine = parsedQuote?.quoteLine || ''
    const text = parsedQuote ? parsedQuote.text : item.message.content
    const hasText = text && text.trim() !== ''
    const ttsAudioPath = item.message.tts_audio_path

    const audioRef = useRef<HTMLAudioElement | null>(null)
    const [ttsPlaying, setTTSPlaying] = useState(false)

    useEffect(() => {
        return () => {
            if (audioRef.current) {
                audioRef.current.pause()
                audioRef.current.src = ''
                audioRef.current = null
            }
        }
    }, [])

    const toggleTTSPlayback = async () => {
        if (!ttsAudioPath) return
        const url = getTTSAudioUrl(ttsAudioPath)

        if (!audioRef.current) {
            const audio = new Audio(url)
            audio.addEventListener('play', () => setTTSPlaying(true))
            audio.addEventListener('pause', () => setTTSPlaying(false))
            audio.addEventListener('ended', () => setTTSPlaying(false))
            audioRef.current = audio
        } else if (audioRef.current.src !== url) {
            audioRef.current.pause()
            audioRef.current.src = url
        }

        try {
            if (audioRef.current.paused) {
                await audioRef.current.play()
            } else {
                audioRef.current.pause()
            }
        } catch {
            setTTSPlaying(false)
        }
    }

    return (
        <div
            className="message-bubble"
            data-bubble-key={item.key}
            onContextMenu={(e) => onContextMenu(e, item)}
            onCopy={(e) => e.preventDefault()}
            onCut={(e) => e.preventDefault()}
            onPointerDown={(e) => onPointerDown(e, item)}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={onPointerCancel}
            onPointerLeave={onPointerLeave}
        >
            <div className="message-content">
                {quoteLine && <div className="message-quote">{quoteLine}</div>}
                <MessageImages
                    timestamp={item.message.timestamp}
                    imagePaths={item.message.image_paths}
                    getImageUrl={getImageUrl}
                />
                {hasText && <div className="message-text">{text}</div>}
                {ttsAudioPath && (
                    <button
                        type="button"
                        className="message-tts-button"
                        onPointerDown={(e) => e.stopPropagation()}
                        onClick={(e) => {
                            e.stopPropagation()
                            void toggleTTSPlayback()
                        }}
                    >
                        <svg viewBox="0 0 24 24" aria-hidden="true">
                            {ttsPlaying ? <path d="M6 5h4v14H6V5zm8 0h4v14h-4V5z" /> : <path d="M8 5v14l11-7L8 5z" />}
                        </svg>
                        <span>{ttsPlaying ? '暂停语音' : '播放语音'}</span>
                    </button>
                )}
            </div>
        </div>
    )
}
