import { createPortal } from 'react-dom'
import type { MessageEditState } from '../types'

interface MessageEditModalProps {
    state: MessageEditState
    onClose: () => void
    onChangeText: (text: string) => void
    onSave: () => void
    saveDisabled: boolean
}

export const MessageEditModal: React.FC<MessageEditModalProps> = ({
    state,
    onClose,
    onChangeText,
    onSave,
    saveDisabled,
}) => {
    return createPortal(
        <div className="message-edit-overlay" onClick={onClose}>
            <div className="message-edit-card" onClick={(e) => e.stopPropagation()}>
                <div className="message-edit-header">
                    <div className="message-edit-title">编辑消息</div>
                    <button type="button" className="message-edit-close" onClick={onClose} aria-label="关闭编辑">
                        ×
                    </button>
                </div>

                {state.quoteLine && <div className="message-edit-quote">{state.quoteLine}</div>}

                <textarea
                    className="message-edit-input"
                    value={state.text}
                    onChange={(e) => onChangeText(e.target.value)}
                    rows={6}
                />

                <div className="message-edit-footer">
                    <button type="button" className="message-edit-btn cancel" onClick={onClose}>
                        取消
                    </button>
                    <button type="button" className="message-edit-btn save" onClick={onSave} disabled={saveDisabled}>
                        保存
                    </button>
                </div>
            </div>
        </div>,
        document.body
    )
}
