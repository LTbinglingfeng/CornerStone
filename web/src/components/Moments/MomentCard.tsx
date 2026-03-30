import { useEffect, useMemo, useRef, useState } from 'react'
import type { UserInfo } from '../../types/chat'
import type { Moment } from '../../types/moments'
import { useT } from '../../contexts/I18nContext'
import { getPromptAvatarUrl } from '../../services/api'
import { likeMoment, unlikeMoment } from '../../services/moments'
import { formatRelativeTime } from '../../utils/time'
import './MomentCard.css'

interface MomentCardProps {
    moment: Moment
    userInfo: UserInfo | null
    onClick: () => void
    onRefresh: () => void
}

function getAvatarInfo(title: string): { text: string; color: string } {
    const colors = ['#4a90d9', '#7ed321', '#bd10e0', '#f5a623', '#50e3c2', '#9013fe', '#417505', '#2b5797']
    const firstChar = title.charAt(0) || '?'
    const colorIndex = firstChar.charCodeAt(0) % colors.length
    return { text: firstChar.toUpperCase(), color: colors[colorIndex] }
}

function normalizeAssetPath(path: string): string {
    const trimmed = path.trim()
    if (!trimmed) return ''
    return trimmed.startsWith('/') ? trimmed : `/${trimmed}`
}

const MomentCard: React.FC<MomentCardProps> = ({ moment, userInfo, onClick, onRefresh }) => {
    const { t } = useT()
    const [showActions, setShowActions] = useState(false)
    const [avatarFailed, setAvatarFailed] = useState(false)
    const actionsRef = useRef<HTMLDivElement>(null)

    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (actionsRef.current && !actionsRef.current.contains(e.target as Node)) {
                setShowActions(false)
            }
        }
        document.addEventListener('mousedown', handleClickOutside)
        return () => document.removeEventListener('mousedown', handleClickOutside)
    }, [])

    const avatar = useMemo(() => getAvatarInfo(moment.prompt_name || 'A'), [moment.prompt_name])
    const avatarSrc = useMemo(
        () => `${getPromptAvatarUrl(moment.prompt_id)}?t=${encodeURIComponent(moment.updated_at)}`,
        [moment.prompt_id, moment.updated_at]
    )

    const userId = userInfo?.username?.trim() || 'user'
    const userName = userInfo?.username?.trim() || t('moments.defaultUser')
    const likes = moment.likes || []
    const comments = moment.comments || []
    const isLiked = likes.some((l) => l.user_type === 'user' && l.user_id === userId)

    const handleLike = async () => {
        if (isLiked) {
            await unlikeMoment(moment.id, 'user', userId)
        } else {
            await likeMoment(moment.id, { user_type: 'user', user_id: userId, user_name: userName })
        }
        setShowActions(false)
        onRefresh()
    }

    const handleComment = () => {
        setShowActions(false)
        onClick()
    }

    const imageSrc = moment.image_path
        ? `${normalizeAssetPath(moment.image_path)}?t=${encodeURIComponent(moment.updated_at)}`
        : ''
    const hasInteractions = likes.length > 0 || comments.length > 0
    const previewComments = comments.slice(0, 3)

    return (
        <div className="moment-card" onClick={onClick}>
            <div className="moment-avatar">
                {!avatarFailed ? (
                    <img
                        src={avatarSrc}
                        alt={moment.prompt_name}
                        onError={() => setAvatarFailed(true)}
                        loading="lazy"
                    />
                ) : (
                    <div className="moment-avatar-placeholder" style={{ backgroundColor: avatar.color }}>
                        {avatar.text}
                    </div>
                )}
            </div>

            <div className="moment-content">
                <div className="moment-name">{moment.prompt_name}</div>
                <div className="moment-text">{moment.content}</div>

                {(moment.status === 'pending' || moment.status === 'generating') && (
                    <div className="moment-image-placeholder">{t('moments.imageGenerating')}</div>
                )}

                {moment.status === 'failed' && (
                    <div className="moment-image-placeholder moment-image-error">
                        {t('moments.imageGenerateFailed', {
                            error: moment.error_msg ? `: ${moment.error_msg}` : '',
                        })}
                    </div>
                )}

                {moment.status === 'published' && imageSrc && (
                    <div className="moment-image">
                        <img src={imageSrc} alt="moment" loading="lazy" />
                    </div>
                )}

                <div className="moment-footer">
                    <span className="moment-time">{formatRelativeTime(moment.created_at)}</span>
                    <div
                        className="moment-actions-wrapper"
                        ref={actionsRef}
                        onClick={(e) => {
                            e.stopPropagation()
                        }}
                    >
                        <button
                            className="moment-actions-trigger"
                            type="button"
                            onClick={() => setShowActions((v) => !v)}
                        >
                            <span className="dot" />
                            <span className="dot" />
                        </button>

                        {showActions && (
                            <div className="moment-actions-menu">
                                <button type="button" onClick={handleLike}>
                                    {isLiked ? t('common.cancel') : t('moments.like')}
                                </button>
                                <button type="button" onClick={handleComment}>
                                    {t('moments.comment')}
                                </button>
                            </div>
                        )}
                    </div>
                </div>

                {hasInteractions && (
                    <div
                        className="moment-interactions"
                        onClick={(e) => {
                            e.stopPropagation()
                            onClick()
                        }}
                    >
                        {likes.length > 0 && (
                            <div className="moment-likes">
                                <span className="moment-like-icon">♥</span>
                                <span className="moment-like-names">{likes.map((l) => l.user_name).join('，')}</span>
                            </div>
                        )}

                        {previewComments.map((comment) => (
                            <div key={comment.id} className="moment-comment-preview">
                                <span className="comment-author">{comment.user_name}</span>
                                <span className="comment-colon">：</span>
                                <span className="comment-content">{comment.content}</span>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    )
}

export default MomentCard
