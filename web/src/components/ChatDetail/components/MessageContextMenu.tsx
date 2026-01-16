import type { ChatMessage } from '../../../types/chat'
import ContextMenu, { type MenuItem } from '../../ContextMenu'
import type { MessageMenuState } from '../types'
import { buildSelectableText, isRecalledMessage } from '../utils'

interface MessageContextMenuProps {
  state: MessageMenuState
  sending: boolean
  onClose: () => void
  onSelectText: (text: string) => void
  onRecall: (messageIndex: number) => void
  onEdit: (messageIndex: number) => void
  onDelete: (messageIndex: number) => void
  onQuote: (message: ChatMessage) => void
}

export const MessageContextMenu: React.FC<MessageContextMenuProps> = ({
  state,
  sending,
  onClose,
  onSelectText,
  onRecall,
  onEdit,
  onDelete,
  onQuote,
}) => {
  const items: MenuItem[] = []
  const selectableText = buildSelectableText(state.message).trim()
  if (selectableText) {
    items.push({ label: '选择文本', onClick: () => onSelectText(selectableText) })
  }

  if (!sending && state.message.role === 'user' && !isRecalledMessage(state.message)) {
    items.push({ label: '撤回', onClick: () => onRecall(state.messageIndex) })
  }

  if (!sending) {
    items.push({ label: '编辑', onClick: () => onEdit(state.messageIndex) })
    items.push({ label: '删除', danger: true, onClick: () => onDelete(state.messageIndex) })
  }

  items.push({ label: '引用', onClick: () => onQuote(state.message) })

  return <ContextMenu items={items} position={state.position} onClose={onClose} />
}

