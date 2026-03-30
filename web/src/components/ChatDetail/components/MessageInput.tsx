import type { QuoteDraft } from '../types'
import { useT } from '../../../contexts/I18nContext'
import { AttachmentPanel } from './AttachmentPanel'

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
        if (e.key === 'Enter' && !e.shiftKey) {
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
            {quoteDraft && (
                <div className="chat-quote-preview">
                    <div className="chat-quote-preview-text">{quoteDraft.line}</div>
                    <button
                        type="button"
                        className="chat-quote-preview-close"
                        onClick={onClearQuote}
                        aria-label={t('chat.closeQuote')}
                    >
                        ×
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
                >
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M19 11H13V5a1 1 0 0 0-2 0v6H5a1 1 0 0 0 0 2h6v6a1 1 0 0 0 2 0v-6h6a1 1 0 0 0 0-2z" />
                    </svg>
                </button>
                <input
                    ref={fileInputRef}
                    className="image-input"
                    type="file"
                    accept="image/*"
                    multiple
                    onChange={onImageChange}
                />
                <textarea
                    ref={textareaRef}
                    className="chat-input"
                    placeholder={t('chat.inputPlaceholder')}
                    value={value}
                    onChange={handleInputChange}
                    onFocus={() => window.setTimeout(onFocusInput, 0)}
                    onKeyDown={handleKeyDown}
                    rows={1}
                />
                <button className="send-button" onClick={onSend} disabled={!canSend}>
                    <svg viewBox="0 0 24 24">
                        <path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" />
                    </svg>
                </button>
            </div>
        </div>
    )
}
