import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useToast } from '../../../contexts/ToastContext'

interface SelectTextModalProps {
    text: string
    onClose: () => void
}

export const SelectTextModal: React.FC<SelectTextModalProps> = ({ text, onClose }) => {
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
            showToast('复制失败，请手动选择文本复制', 'error')
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

    return createPortal(
        <div className="select-text-overlay" onClick={onClose}>
            <div className="select-text-card" onClick={(e) => e.stopPropagation()}>
                <div className="select-text-header">
                    <div className="select-text-title">选择文本</div>
                    <button type="button" className="select-text-close" onClick={onClose} aria-label="关闭选择文本">
                        ×
                    </button>
                </div>

                <textarea ref={textareaRef} className="select-text-textarea" value={text} readOnly rows={6} />

                <div className="select-text-footer">
                    {copied && <div className="select-text-hint">已复制</div>}
                    <button type="button" className="select-text-btn copy" onClick={handleCopy}>
                        复制
                    </button>
                    <button type="button" className="select-text-btn" onClick={onClose}>
                        关闭
                    </button>
                </div>
            </div>
        </div>,
        document.body
    )
}
