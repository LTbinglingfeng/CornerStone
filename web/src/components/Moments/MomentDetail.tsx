import { useCallback, useEffect, useMemo, useState } from 'react'
import type { UserInfo } from '../../types/chat'
import type { Moment } from '../../types/moments'
import { useT } from '../../contexts/I18nContext'
import { getPromptAvatarUrl } from '../../services/api'
import { addComment, getMoment, likeMoment, unlikeMoment } from '../../services/moments'
import { formatRelativeTime } from '../../utils/time'
import './MomentDetail.css'

interface MomentDetailProps {
    moment: Moment
    userInfo: UserInfo | null
    onClose: () => void
    onRefresh: () => void | Promise<void>
}

function normalizeAssetPath(path: string): string {
    const trimmed = path.trim()
    if (!trimmed) return ''
    return trimmed.startsWith('/') ? trimmed : `/${trimmed}`
}

const MomentDetail: React.FC<MomentDetailProps> = ({ moment, userInfo, onClose, onRefresh }) => {
    const { t } = useT()
    const [data, setData] = useState<Moment>(moment)
    const [loading, setLoading] = useState(true)
    const [commentText, setCommentText] = useState('')
    const [submitting, setSubmitting] = useState(false)

    const userId = userInfo?.username?.trim() || 'user'
    const userName = userInfo?.username?.trim() || t('moments.defaultUser')
    const likes = data.likes || []
    const comments = data.comments || []
    const isLiked = likes.some((l) => l.user_type === 'user' && l.user_id === userId)

    const refreshMoment = useCallback(async () => {
        const updated = await getMoment(moment.id)
        if (updated) {
            setData(updated)
        }
    }, [moment.id])

    useEffect(() => {
        setData(moment)
        setLoading(true)
        ;(async () => {
            await refreshMoment()
            setLoading(false)
        })()
    }, [moment, refreshMoment])

    const avatarSrc = useMemo(
        () => `${getPromptAvatarUrl(data.prompt_id)}?t=${encodeURIComponent(data.updated_at)}`,
        [data.prompt_id, data.updated_at]
    )
    const imageSrc = data.image_path
        ? `${normalizeAssetPath(data.image_path)}?t=${encodeURIComponent(data.updated_at)}`
        : ''

    const handleLike = async () => {
        setSubmitting(true)
        try {
            if (isLiked) {
                await unlikeMoment(data.id, 'user', userId)
            } else {
                await likeMoment(data.id, { user_type: 'user', user_id: userId, user_name: userName })
            }
            await refreshMoment()
            await onRefresh()
        } finally {
            setSubmitting(false)
        }
    }

    const handleSendComment = async () => {
        const content = commentText.trim()
        if (!content) return

        setSubmitting(true)
        try {
            await addComment(data.id, { user_type: 'user', user_id: userId, user_name: userName, content })
            setCommentText('')
            await refreshMoment()
            await onRefresh()
        } finally {
            setSubmitting(false)
        }
    }

    return (
        <div
            className="moment-detail-overlay"
            onClick={(e) => {
                if (e.target === e.currentTarget) onClose()
            }}
        >
            <div className="moment-detail">
                <div className="moment-detail-header">
                    <button className="moment-detail-close" type="button" onClick={onClose}>
                        {t('common.close')}
                    </button>
                    <div className="moment-detail-title">{t('common.details')}</div>
                    <div className="moment-detail-spacer" />
                </div>

                <div className="moment-detail-body">
                    {loading ? (
                        <div className="moment-detail-loading">{t('common.loading')}</div>
                    ) : (
                        <>
                            <div className="moment-detail-main">
                                <div className="moment-detail-avatar">
                                    <img src={avatarSrc} alt={data.prompt_name} loading="lazy" />
                                </div>
                                <div className="moment-detail-content">
                                    <div className="moment-detail-name">{data.prompt_name}</div>
                                    <div className="moment-detail-text">{data.content}</div>

                                    {(data.status === 'pending' || data.status === 'generating') && (
                                        <div className="moment-detail-image-placeholder">
                                            {t('moments.imageGenerating')}
                                        </div>
                                    )}
                                    {data.status === 'failed' && (
                                        <div className="moment-detail-image-placeholder moment-detail-image-error">
                                            {t('moments.imageGenerateFailed', {
                                                error: data.error_msg ? `: ${data.error_msg}` : '',
                                            })}
                                        </div>
                                    )}
                                    {data.status === 'published' && imageSrc && (
                                        <div className="moment-detail-image">
                                            <img src={imageSrc} alt="moment" loading="lazy" />
                                        </div>
                                    )}

                                    <div className="moment-detail-meta">
                                        <span className="moment-detail-time">
                                            {formatRelativeTime(data.created_at)}
                                        </span>
                                        <button
                                            className={`moment-detail-like ${isLiked ? 'liked' : ''}`}
                                            type="button"
                                            onClick={handleLike}
                                            disabled={submitting}
                                        >
                                            {isLiked ? t('moments.liked') : t('moments.like')}
                                        </button>
                                    </div>
                                </div>
                            </div>

                            <div className="moment-detail-interactions">
                                {likes.length > 0 && (
                                    <div className="moment-detail-likes">
                                        <span className="moment-detail-like-icon">♥</span>
                                        <span className="moment-detail-like-names">
                                            {likes.map((l) => l.user_name).join('，')}
                                        </span>
                                    </div>
                                )}

                                {comments.length > 0 ? (
                                    <div className="moment-detail-comments">
                                        {comments.map((comment) => (
                                            <div key={comment.id} className="moment-detail-comment">
                                                <span className="moment-detail-comment-author">
                                                    {comment.user_name}
                                                </span>
                                                <span className="moment-detail-comment-colon">：</span>
                                                <span className="moment-detail-comment-content">{comment.content}</span>
                                            </div>
                                        ))}
                                    </div>
                                ) : (
                                    <div className="moment-detail-empty">{t('moments.noComments')}</div>
                                )}
                            </div>
                        </>
                    )}
                </div>

                <div className="moment-detail-editor">
                    <input
                        className="moment-detail-input"
                        value={commentText}
                        onChange={(e) => setCommentText(e.target.value)}
                        placeholder={t('moments.commentPlaceholder')}
                        disabled={submitting}
                    />
                    <button
                        className="moment-detail-send"
                        type="button"
                        onClick={handleSendComment}
                        disabled={submitting || commentText.trim() === ''}
                    >
                        {t('common.send')}
                    </button>
                </div>
            </div>
        </div>
    )
}

export default MomentDetail
