import { motion } from 'motion/react'
import { useT } from '../../../contexts/I18nContext'
import { centerModalVariants, overlayVariants } from '../../../utils/motion'
import { ModalOverlay } from '../../ModalOverlay'
import { Icon } from '../../ui'
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
    return (
        <ModalOverlay
            className="prompt-modal-overlay"
            aria-label={t('chat.editMessage')}
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={overlayVariants}
            onDismiss={onClose}
        >
            <motion.div className="prompt-modal-card" variants={centerModalVariants}>
                <div className="prompt-modal-header">
                    <h3>{t('chat.editMessage')}</h3>
                    <button
                        type="button"
                        className="prompt-modal-close"
                        onClick={onClose}
                        aria-label={t('common.close')}
                    >
                        <Icon name="x" />
                    </button>
                </div>

                <div className="prompt-modal-body">
                    {state.quoteLine && <div className="message-edit-quote">{state.quoteLine}</div>}
                    <textarea
                        className="prompt-modal-textarea message-edit-input"
                        aria-label={t('chat.editMessage')}
                        value={state.text}
                        onChange={(e) => onChangeText(e.target.value)}
                        rows={6}
                        autoFocus
                    />
                </div>

                <div className="prompt-modal-footer">
                    <button type="button" className="prompt-modal-btn cancel" onClick={onClose}>
                        {t('common.cancel')}
                    </button>
                    <button type="button" className="prompt-modal-btn save" onClick={onSave} disabled={saveDisabled}>
                        {t('common.save')}
                    </button>
                </div>
            </motion.div>
        </ModalOverlay>
    )
}
