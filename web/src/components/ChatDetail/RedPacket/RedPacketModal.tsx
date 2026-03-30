import type { Prompt, UserInfo } from '../../../types/chat'
import { appendQueryParam, getPromptAvatarUrl } from '../../../services/api'
import { useT } from '../../../contexts/I18nContext'
import type { ActiveRedPacketState } from '../types'
import type { PacketStep, RedPacketReceivedRecord } from './types'
import { formatRedPacketTime } from './utils'

interface RedPacketModalProps {
    activeRedPacket: ActiveRedPacketState
    packetStep: PacketStep
    onOpen: () => void
    onClose: () => void
    userInfo: UserInfo | null
    prompt: Prompt | null
    getReceivedRecord: (packetKey: string) => RedPacketReceivedRecord | null
}

export const RedPacketModal: React.FC<RedPacketModalProps> = ({
    activeRedPacket,
    packetStep,
    onOpen,
    onClose,
    userInfo,
    prompt,
    getReceivedRecord,
}) => {
    const { t } = useT()
    const received = getReceivedRecord(activeRedPacket.packetKey)
    const senderName = activeRedPacket.senderName
    const senderMessage = activeRedPacket.params.message || t('redPacket.defaultGreeting')

    const getPromptAvatarSrc = () => {
        if (prompt?.avatar) {
            return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
        }
        return null
    }

    if (packetStep === 'opened' && activeRedPacket.senderRole === 'user') {
        const receiverName = prompt?.name?.trim() || t('chat.defaultAIName')
        const receiverTime = received?.timestamp ? formatRedPacketTime(received.timestamp) : ''
        const receiverAvatarSrc = getPromptAvatarSrc()
        return (
            <div className="rp-detail-overlay">
                <div className="rp-detail-top">
                    <div className="rp-detail-nav">
                        <button
                            type="button"
                            className="rp-detail-back"
                            onClick={onClose}
                            aria-label={t('common.back')}
                        >
                            <svg viewBox="0 0 24 24" aria-hidden="true">
                                <path d="M15.5 5.5a1 1 0 0 1 0 1.4L10.4 12l5.1 5.1a1 1 0 1 1-1.4 1.4l-5.8-5.8a1 1 0 0 1 0-1.4l5.8-5.8a1 1 0 0 1 1.4 0z" />
                            </svg>
                        </button>
                        <div className="rp-detail-nav-spacer" />
                    </div>

                    <div className="rp-detail-header">
                        {activeRedPacket.senderAvatarSrc ? (
                            <img className="rp-detail-avatar" src={activeRedPacket.senderAvatarSrc} alt="avatar" />
                        ) : (
                            <div className="rp-detail-avatar placeholder">
                                {senderName.charAt(0)?.toUpperCase() || 'U'}
                            </div>
                        )}
                        <div className="rp-detail-title">
                            {senderName}
                            {t('redPacket.senderRedPacket')}
                        </div>
                        <div className="rp-detail-message">{senderMessage}</div>
                    </div>
                </div>

                <div className="rp-detail-body">
                    <div className="rp-detail-summary">
                        {t('redPacket.packetSummary', { amount: activeRedPacket.params.amount.toFixed(2) })}
                    </div>

                    <div className="rp-detail-list">
                        <div className="rp-detail-item">
                            {receiverAvatarSrc ? (
                                <img className="rp-detail-item-avatar" src={receiverAvatarSrc} alt="avatar" />
                            ) : (
                                <div className="rp-detail-item-avatar placeholder">
                                    {receiverName.charAt(0)?.toUpperCase() || 'A'}
                                </div>
                            )}
                            <div className="rp-detail-item-main">
                                <div className="rp-detail-item-name">{receiverName}</div>
                                <div className="rp-detail-item-time">{receiverTime || t('redPacket.notClaimed')}</div>
                            </div>
                            <div className="rp-detail-item-amount">
                                {activeRedPacket.params.amount.toFixed(2)} {t('redPacket.yuan')}
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        )
    }

    const receiverName =
        received?.receiverName ||
        (activeRedPacket.senderRole === 'assistant'
            ? userInfo?.username?.trim() || t('chat.defaultTargetName')
            : prompt?.name?.trim() || t('chat.defaultAIName'))

    return (
        <div className="rp-modal-overlay">
            <div className={`rp-modal ${packetStep === 'opened' ? 'opened' : ''}`}>
                <button className="rp-close-btn" onClick={onClose}>
                    ×
                </button>

                {packetStep !== 'opened' ? (
                    <div className="rp-modal-front">
                        <div className="rp-modal-top">
                            <div className="rp-sender-row">
                                {activeRedPacket.senderAvatarSrc ? (
                                    <img src={activeRedPacket.senderAvatarSrc} className="rp-avatar-img" alt="avatar" />
                                ) : (
                                    <div className="rp-avatar-placeholder">
                                        {senderName.charAt(0)?.toUpperCase() || 'A'}
                                    </div>
                                )}
                                <span className="rp-sender-name">{senderName}</span>
                            </div>
                            <div className="rp-wishing">{senderMessage}</div>
                        </div>
                        <div className="rp-modal-open-btn-wrapper">
                            <button
                                className={`rp-open-btn ${packetStep === 'opening' ? 'opening' : ''}`}
                                onClick={onOpen}
                            >
                                {t('redPacket.openButton')}
                            </button>
                        </div>
                    </div>
                ) : (
                    <div className="rp-modal-result">
                        <div className="rp-result-header">
                            <div className="rp-result-top-bg"></div>
                            <div className="rp-sender-row small">
                                {activeRedPacket.senderAvatarSrc ? (
                                    <img
                                        src={activeRedPacket.senderAvatarSrc}
                                        className="rp-avatar-img small"
                                        alt="avatar"
                                    />
                                ) : (
                                    <div className="rp-avatar-placeholder small">
                                        {senderName.charAt(0)?.toUpperCase() || 'A'}
                                    </div>
                                )}
                                <span className="rp-sender-name dark">
                                    {senderName}
                                    {t('redPacket.senderRedPacket')}
                                </span>
                            </div>
                            <div className="rp-wishing dark">{senderMessage}</div>
                        </div>

                        <div className="rp-result-amount">
                            <span className="rp-currency">¥</span>
                            <span className="rp-num">{activeRedPacket.params.amount.toFixed(2)}</span>
                        </div>

                        <div className="rp-result-footer">
                            <div className="rp-result-meta">
                                <div className="rp-result-meta-row">
                                    <span className="rp-result-meta-label">{t('redPacket.recipient')}</span>
                                    <span className="rp-result-meta-value">{receiverName}</span>
                                </div>
                                <div className="rp-result-meta-hint">
                                    {activeRedPacket.senderRole === 'assistant'
                                        ? t('redPacket.savedToWallet')
                                        : t('redPacket.claimedBy', { name: receiverName })}
                                </div>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </div>
    )
}
