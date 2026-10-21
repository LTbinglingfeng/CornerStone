import { useEffect, useRef, useState } from 'react'
import { motion } from 'motion/react'
import { useT } from '../../../contexts/I18nContext'
import { useToast } from '../../../contexts/ToastContext'
import { centerModalVariants, overlayVariants } from '../../../utils/motion'
import { ModalOverlay } from '../../ModalOverlay'
import { Icon } from '../../ui'

interface SelectTextModalProps {
    text: string
    onClose: () => void
}

export const SelectTextModal: React.FC<SelectTextModalProps> = ({ text, onClose }) => {
    const { t } = useT()
    const { showToast } = useToast()
    const textareaRef = useRef<HTMLTextAreaElement>(null)
    const [copied, setCopied] = useState(false)
    const copiedTimeoutRef = useRef<number | null>(null)

    useEffect(() => {
        setCopied(false)
        window.setTimeout(() => {
            const textarea = textareaRef.current
            if (!textarea) return
            textarea.focus()
            textarea.select()
        }, 0)
        return () => {
            if (copiedTimeoutRef.current !== null) {
                window.clearTimeout(copiedTimeoutRef.current)
                copiedTimeoutRef.current = null
            }
        }
    }, [text])

    const handleCopy = async () => {
        let copiedOk = false
        if (navigator.clipboard?.writeText) {
            try {
                await navigator.clipboard.writeText(text)
                copiedOk = true
            } catch {
                copiedOk = false
            }
        }

        if (!copiedOk) {
            const textarea = textareaRef.current
            if (textarea) {
                textarea.focus()
                textarea.select()
                try {
                    copiedOk = document.execCommand('copy')
                } catch {
                    copiedOk = false
                }
            }
        }

        if (!copiedOk) {
            showToast(t('chat.copyFailed'), 'error')
            return
        }

        setCopied(true)
        if (copiedTimeoutRef.current !== null) {
            window.clearTimeout(copiedTimeoutRef.current)
        }
        copiedTimeoutRef.current = window.setTimeout(() => {
            copiedTimeoutRef.current = null
            setCopied(false)
        }, 1500)
    }

    return (
        <ModalOverlay
            className="prompt-modal-overlay"
            aria-label={t('chat.selectText')}
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={overlayVariants}
            onDismiss={onClose}
        >
            <motion.div className="prompt-modal-card" variants={centerModalVariants}>
                <div className="prompt-modal-header">
                    <h3>{t('chat.selectText')}</h3>
                    <button
                        type="button"
                        className="prompt-modal-close"
                        onClick={onClose}
                        aria-label={t('chat.closeSelectText')}
                    >
                        <Icon name="x" />
                    </button>
                </div>

                <div className="prompt-modal-body">
                    <textarea
                        ref={textareaRef}
                        className="prompt-modal-textarea select-text-textarea"
                        aria-label={t('chat.selectText')}
                        value={text}
                        readOnly
                        rows={6}
                    />
                </div>

                <div className="prompt-modal-footer">
                    <span className="select-text-hint" role="status">
                        {copied ? t('common.copied') : ''}
                    </span>
                    <button type="button" className="prompt-modal-btn cancel" onClick={onClose}>
                        {t('common.close')}
                    </button>
                    <button type="button" className="prompt-modal-btn save" onClick={handleCopy}>
                        {copied ? <Icon name="check" size={16} /> : null}
                        {t('common.copy')}
                    </button>
                </div>
            </motion.div>
        </ModalOverlay>
    )
}
