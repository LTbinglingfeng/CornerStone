interface ChatHeaderProps {
  title: string
  sending: boolean
  assistantVisibleSegments: number
  showSettingsButton: boolean
  onBack: () => void
  onOpenSettings: () => void
}

export const ChatHeader: React.FC<ChatHeaderProps> = ({
  title,
  sending,
  assistantVisibleSegments,
  showSettingsButton,
  onBack,
  onOpenSettings,
}) => {
  const showTyping = sending && assistantVisibleSegments === 0

  return (
    <div className="chat-detail-header">
      <button className="back-button" onClick={onBack}>
        <svg viewBox="0 0 24 24">
          <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
        </svg>
      </button>
      <div className={`chat-detail-title${showTyping ? ' typing' : ''}`}>
        {showTyping ? '对方正在输入中…' : title}
      </div>
      {showSettingsButton && (
        <button className="settings-button" onClick={onOpenSettings}>
          <svg viewBox="0 0 24 24">
            <path d="M6 10c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm6 0c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm6 0c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2z" />
          </svg>
        </button>
      )}
    </div>
  )
}

