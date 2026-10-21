import type { QuoteDraft } from '../types'
import { useT } from '../../../contexts/I18nContext'
import { AttachmentPanel } from './AttachmentPanel'
import { Icon } from '../../ui'

interface MessageInputProps {
    value: string
    onChange: (value: string) => void
    onSend: () => void
    canSend: boolean
    quoteDraft: QuoteDraft | null
    onClearQuote: () => void
    showAttachmentMenu: boolean
    onToggleAttachmentMenu: () => void
    onCloseAttachmentMenu: () => void
    onUploadClick: () => void
    onOpenRedPacket: () => void
    onImageChange: (e: React.ChangeEvent<HTMLInputElement>) => void
    imageCapable: boolean
    uploadingImage: boolean
    sending: boolean
    textareaRef: React.RefObject<HTMLTextAreaElement | null>
    fileInputRef: React.RefObject<HTMLInputElement | null>
    attachmentButtonRef: React.RefObject<HTMLButtonElement | null>
    attachmentPanelRef: React.RefObject<HTMLDivElement | null>
    onFocusInput: () => void
}

export const MessageInput: React.FC<MessageInputProps> = ({
    value,
    onChange,
    onSend,
    canSend,
    quoteDraft,
    onClearQuote,
    showAttachmentMenu,
    onToggleAttachmentMenu,
    onCloseAttachmentMenu,
    onUploadClick,
    onOpenRedPacket,
    onImageChange,
    imageCapable,
    uploadingImage,
    sending,
    textareaRef,
    fileInputRef,
    attachmentButtonRef,
    attachmentPanelRef,
    onFocusInput,
}) => {
    const { t } = useT()
    const handleKeyDown = (e: React.KeyboardEvent) => {
        // Ignore Enter while an IME is composing (e.g. confirming a Chinese candidate).
        if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault()
            onSend()
        }
    }

    const handleInputChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
        onChange(e.target.value)
        const textarea = e.target
        textarea.style.height = '36px'
        textarea.style.height = Math.min(textarea.scrollHeight, 120) + 'px'
    }

    return (
        <div className="chat-input-area">
            <div className="chat-composer">
                {quoteDraft && (
                    <div className="chat-quote-preview">
                        <div className="chat-quote-preview-text">{quoteDraft.line}</div>
                        <button
                            type="button"
                            className="chat-quote-preview-close"
                            onClick={onClearQuote}
                            aria-label={t('chat.closeQuote')}
                        >
                            <Icon name="x" size={14} />
                        </button>
                    </div>
                )}

                <AttachmentPanel
                    open={showAttachmentMenu}
                    panelRef={attachmentPanelRef}
                    imageCapable={imageCapable}
                    uploadingImage={uploadingImage}
                    sending={sending}
                    onClose={onCloseAttachmentMenu}
                    onUpload={onUploadClick}
                    onOpenRedPacket={onOpenRedPacket}
                />

                <div className="chat-input-row">
                    <button
                        ref={attachmentButtonRef}
                        type="button"
                        className={`attachment-button ${showAttachmentMenu ? 'open' : ''}`}
                        onClick={onToggleAttachmentMenu}
                        aria-label={t('chat.moreFeatures')}
                        aria-haspopup="menu"
                        aria-expanded={showAttachmentMenu}
                    >
                        {uploadingImage ? (
                            <span className="ui-spinner" aria-hidden="true" />
                        ) : (
                            <Icon name="plus" size={20} />
                        )}
                    </button>
                    <input
                        ref={fileInputRef}
                        className="image-input"
                        type="file"
                        accept="image/*"
                        multiple
                        onChange={onImageChange}
                        tabIndex={-1}
                        aria-hidden="true"
                    />
                    <textarea
                        ref={textareaRef}
                        className="chat-input"
                        aria-label={t('chat.inputPlaceholder')}
                        placeholder={t('chat.inputPlaceholder')}
                        value={value}
                        onChange={handleInputChange}
                        onFocus={() => window.setTimeout(onFocusInput, 0)}
                        onKeyDown={handleKeyDown}
                        rows={1}
                    />
                    <button
                        type="button"
                        className="send-button"
                        onClick={onSend}
                        disabled={!canSend}
                        aria-label={t('common.send')}
                    >
                        <svg
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="2.25"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            aria-hidden="true"
                        >
                            <path d="M12 19V5" />
                            <path d="m5.5 11.5 6.5-6.5 6.5 6.5" />
                        </svg>
                    </button>
                </div>
            </div>
        </div>
    )
}
