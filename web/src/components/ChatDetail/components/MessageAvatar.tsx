interface MessageAvatarProps {
    role: 'user' | 'assistant'
    avatarSrc: string | null
    placeholder: string
    onDoubleClick?: (e: React.MouseEvent) => void
    title?: string
}

export const MessageAvatar: React.FC<MessageAvatarProps> = ({ role, avatarSrc, placeholder, onDoubleClick, title }) => {
    const { t } = useT()
    return (
        <div className="message-avatar" onDoubleClick={onDoubleClick} title={title}>
            {avatarSrc ? (
                <img src={avatarSrc} alt={role === 'user' ? t('chat.userAvatar') : t('chat.aiAvatar')} />
            ) : (
                <div className={`avatar-placeholder ${role}`}>{placeholder}</div>
            )}
        </div>
    )
}
import { useT } from '../../../contexts/I18nContext'
