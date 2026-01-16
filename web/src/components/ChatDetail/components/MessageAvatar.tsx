interface MessageAvatarProps {
  role: 'user' | 'assistant'
  avatarSrc: string | null
  placeholder: string
  onDoubleClick?: (e: React.MouseEvent) => void
  title?: string
}

export const MessageAvatar: React.FC<MessageAvatarProps> = ({ role, avatarSrc, placeholder, onDoubleClick, title }) => {
  return (
    <div className="message-avatar" onDoubleClick={onDoubleClick} title={title}>
      {avatarSrc ? (
        <img src={avatarSrc} alt={role === 'user' ? '用户' : 'AI'} />
      ) : (
        <div className={`avatar-placeholder ${role}`}>{placeholder}</div>
      )}
    </div>
  )
}

