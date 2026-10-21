import { useT } from '../../../contexts/I18nContext'
import { Icon } from '../../ui'

interface AttachmentPanelProps {
    open: boolean
    panelRef: React.RefObject<HTMLDivElement | null>
    imageCapable: boolean
    uploadingImage: boolean
    sending: boolean
    onClose: () => void
    onUpload: () => void
    onOpenRedPacket: () => void
}

/** Small popover anchored to the composer's attach button. */
export const AttachmentPanel: React.FC<AttachmentPanelProps> = ({
    open,
    panelRef,
    imageCapable,
    uploadingImage,
    sending,
    onClose,
    onUpload,
    onOpenRedPacket,
}) => {
    const { t } = useT()
    if (!open) return null

    return (
        <div
            className="attachment-expand-panel"
            ref={panelRef}
            role="menu"
            onKeyDown={(event) => {
                if (event.key === 'Escape') {
                    event.stopPropagation()
                    onClose()
                }
            }}
        >
            <button
                type="button"
                className="attachment-tile"
                onClick={() => {
                    onClose()
                    onUpload()
                }}
                disabled={!imageCapable || uploadingImage}
                role="menuitem"
            >
                <span className="attachment-tile-icon" aria-hidden="true">
                    <Icon name="image" size={16} />
                </span>
                <span className="attachment-tile-label">{t('chat.album')}</span>
            </button>

            <button
                type="button"
                className="attachment-tile"
                onClick={() => {
                    onClose()
                    onOpenRedPacket()
                }}
                disabled={sending}
                role="menuitem"
            >
                <span className="attachment-tile-icon attachment-tile-icon--packet" aria-hidden="true">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round">
                        <rect x="5" y="3" width="14" height="18" rx="2.5" />
                        <path d="M5 8.5c2 1.6 4.4 2.5 7 2.5s5-.9 7-2.5" />
                        <circle cx="12" cy="11" r="1.6" fill="currentColor" stroke="none" />
                    </svg>
                </span>
                <span className="attachment-tile-label">{t('chat.redPacket')}</span>
            </button>
        </div>
    )
}
