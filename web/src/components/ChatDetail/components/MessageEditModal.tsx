import { createPortal } from 'react-dom'
import { useT } from '../../../contexts/I18nContext'
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
    const { t } = useT()
    return createPortal(
        <div className="message-edit-overlay" onClick={onClose}>
            <div className="message-edit-card" onClick={(e) => e.stopPropagation()}>
                <div className="message-edit-header">
                    <div className="message-edit-title">{t('chat.editMessage')}</div>
                    <button
                        type="button"
                        className="message-edit-close"
                        onClick={onClose}
                        aria-label={t('common.close')}
                    >
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
                        {t('common.cancel')}
                    </button>
                    <button type="button" className="message-edit-btn save" onClick={onSave} disabled={saveDisabled}>
                        {t('common.save')}
                    </button>
                </div>
            </div>
        </div>,
        document.body
    )
}
