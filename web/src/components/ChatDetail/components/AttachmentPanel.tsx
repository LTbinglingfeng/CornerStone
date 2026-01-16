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
  if (!open) return null

  return (
    <div className="attachment-expand-panel" ref={panelRef} role="menu">
      <div className="attachment-grid">
        <button
          type="button"
          className="attachment-tile"
          onClick={() => {
            onClose()
            onUpload()
          }}
          disabled={!imageCapable || uploadingImage}
          aria-label="相册"
          role="menuitem"
        >
          <div className="attachment-tile-icon">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M19 7h-3V5c0-1.1-.9-2-2-2h-4c-1.1 0-2 .9-2 2v2H5c-1.1 0-2 .9-2 2v9c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V9c0-1.1-.9-2-2-2zm-9 0V5h4v2h-4zm7 13H5V9h14v11zM7 18l3-4 2 3 3-4 2 5H7z" />
            </svg>
          </div>
          <div className="attachment-tile-label">相册</div>
        </button>

        <button
          type="button"
          className="attachment-tile"
          onClick={() => {
            onClose()
            onOpenRedPacket()
          }}
          disabled={sending}
          aria-label="红包"
          role="menuitem"
        >
          <div className="attachment-tile-icon">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M7 3h10a2 2 0 0 1 2 2v4a3 3 0 0 1-3 3H8a3 3 0 0 1-3-3V5a2 2 0 0 1 2-2zm0 12h10a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-2a2 2 0 0 1 2-2zm5-10a1.2 1.2 0 1 0 0 2.4A1.2 1.2 0 0 0 12 5zm0 12a1.2 1.2 0 1 0 0 2.4A1.2 1.2 0 0 0 12 17z" />
            </svg>
          </div>
          <div className="attachment-tile-label">红包</div>
        </button>
      </div>
    </div>
  )
}

