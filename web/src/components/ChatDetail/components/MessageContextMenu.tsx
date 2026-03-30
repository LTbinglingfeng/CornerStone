import type { ChatMessage } from '../../../types/chat'
import { useT } from '../../../contexts/I18nContext'
import ContextMenu, { type MenuItem } from '../../ContextMenu'
import type { MessageMenuState } from '../types'
import { buildSelectableText, isRecalledMessage } from '../utils'

interface MessageContextMenuProps {
    state: MessageMenuState
    sending: boolean
    messages: ChatMessage[]
    onClose: () => void
    onSelectText: (text: string) => void
    onRecall: (messageIndex: number) => void
    onEdit: (messageIndex: number) => void
    onDelete: (messageIndex: number) => void
    onQuote: (message: ChatMessage) => void
    onRegenerate: () => void
}

export const MessageContextMenu: React.FC<MessageContextMenuProps> = ({
    state,
    sending,
    messages,
    onClose,
    onSelectText,
    onRecall,
    onEdit,
    onDelete,
    onQuote,
    onRegenerate,
}) => {
    const { t } = useT()
    const items: MenuItem[] = []
    const selectableText = buildSelectableText(state.message).trim()
    if (selectableText) {
        items.push({ label: t('chat.selectText'), onClick: () => onSelectText(selectableText) })
    }

    if (!sending && state.message.role === 'user' && !isRecalledMessage(state.message)) {
        items.push({ label: t('chat.recall'), onClick: () => onRecall(state.messageIndex) })
    }

    // 重新生成：仅对尾部连续 assistant 批次中的消息显示
    if (!sending && state.message.role === 'assistant') {
        const n = messages.length
        if (n > 0 && messages[n - 1].role === 'assistant') {
            let batchStart = n - 1
            while (batchStart > 0 && messages[batchStart - 1].role === 'assistant') {
                batchStart--
            }
            if (state.messageIndex >= batchStart) {
                items.push({ label: t('chat.regenerate'), onClick: () => onRegenerate() })
            }
        }
    }

    if (!sending) {
        items.push({ label: t('common.edit'), onClick: () => onEdit(state.messageIndex) })
        items.push({ label: t('common.delete'), danger: true, onClick: () => onDelete(state.messageIndex) })
    }

    items.push({ label: t('chat.quote'), onClick: () => onQuote(state.message) })

    return <ContextMenu items={items} position={state.position} onClose={onClose} />
}
