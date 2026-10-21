import { useT } from '../../../contexts/I18nContext'
import { getManagementCopy } from '../../../i18n/archive'
import { Avatar, Icon } from '../../ui'

interface ChatHeaderProps {
    title: string
    personaName: string
    avatarSrc: string | null
    sending: boolean
    assistantVisibleSegments: number
    showSettingsButton: boolean
    onBack: () => void
    onOpenSettings: () => void
}

export const ChatHeader: React.FC<ChatHeaderProps> = ({
    title,
    personaName,
    avatarSrc,
    sending,
    assistantVisibleSegments,
    showSettingsButton,
    onBack,
    onOpenSettings,
}) => {
    const { t, locale } = useT()
    const copy = getManagementCopy(locale).conversation
    const showTyping = sending && assistantVisibleSegments === 0

    return (
        <header className="chat-detail-header">
            <button
                type="button"
                className="back-button chat-detail-back"
                onClick={onBack}
                aria-label={t('common.back')}
            >
                <Icon name="arrowLeft" size={20} />
            </button>
            <Avatar src={avatarSrc} name={personaName} size="sm" className="chat-detail-avatar" />
            <div className="chat-detail-heading">
                <h2 className="chat-detail-title">{title}</h2>
                <p className={`chat-detail-subtitle${showTyping ? ' typing' : ''}`} aria-live="polite">
                    {showTyping ? t('chat.typingIndicator') : personaName}
                </p>
            </div>
            {showSettingsButton && (
                <button
                    type="button"
                    className="ui-icon-btn settings-button"
                    onClick={onOpenSettings}
                    aria-label={copy.more}
                    title={copy.more}
                >
                    <Icon name="more" />
                </button>
            )}
        </header>
    )
}
