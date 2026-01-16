import type { DisplayItem } from '../types'
import { parseQuotedMessageContent } from '../utils'
import { MessageImages } from './MessageImages'

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

  return (
    <div
      className="message-bubble"
      data-bubble-key={item.key}
      onContextMenu={e => onContextMenu(e, item)}
      onCopy={e => e.preventDefault()}
      onCut={e => e.preventDefault()}
      onPointerDown={e => onPointerDown(e, item)}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
      onPointerLeave={onPointerLeave}
    >
      <div className="message-content">
        {quoteLine && <div className="message-quote">{quoteLine}</div>}
        <MessageImages timestamp={item.message.timestamp} imagePaths={item.message.image_paths} getImageUrl={getImageUrl} />
        {hasText && <div className="message-text">{text}</div>}
      </div>
    </div>
  )
}

