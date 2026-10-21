import { useState } from 'react'
import type { ChatAttachment } from '../../../types/chat'
import { getAuthToken } from '../../../services/api'
import { useT } from '../../../contexts/I18nContext'
import { Icon } from '../../ui'

export function MessageAttachments({ attachments }: { attachments?: ChatAttachment[] }) {
    const { t } = useT()
    const [error, setError] = useState(false)
    const [busy, setBusy] = useState(false)
    const download = async (attachment: ChatAttachment) => {
        if (!/^\/api\/attachments\/[0-9a-f-]{36}$/.test(attachment.path) || busy) return
        setBusy(true)
        setError(false)
        try {
            const token = getAuthToken()
            const response = await fetch(attachment.path, {
                headers: token ? { Authorization: `Bearer ${token}` } : {},
            })
            if (!response.ok) throw new Error('download failed')
            const url = URL.createObjectURL(await response.blob())
            const link = document.createElement('a')
            link.href = url
            link.download = attachment.name
            link.click()
            window.setTimeout(() => URL.revokeObjectURL(url), 1000)
        } catch {
            setError(true)
        } finally {
            setBusy(false)
        }
    }
    if (!attachments?.length && !error) return null
    return (
        <div className="message-attachments">
            {attachments?.map((attachment) => (
                <div key={attachment.path} className="message-attachment">
                    <button
                        type="button"
                        className="message-attachment-btn"
                        disabled={busy}
                        onPointerDown={(e) => e.stopPropagation()}
                        onClick={() => void download(attachment)}
                        aria-label={`${t('chat.downloadAttachment')}: ${attachment.name}`}
                    >
                        <span className="message-attachment-icon" aria-hidden="true">
                            <Icon name="fileText" size={18} />
                        </span>
                        <span className="message-attachment-info">
                            <span className="message-attachment-name">{attachment.name}</span>
                            <span className="message-attachment-size">
                                {Math.ceil(attachment.size / 1024)} KB · {t('chat.downloadAttachment')}
                            </span>
                        </span>
                    </button>
                    <small className="message-attachment-note">{t('chat.attachmentModelFallback')}</small>
                </div>
            ))}
            {error && (
                <span className="message-attachment-error" role="alert">
                    {t('chat.attachmentDownloadFailed')}
                </span>
            )}
        </div>
    )
}
