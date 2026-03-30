import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useT } from '../../../contexts/I18nContext'

interface RedPacketComposerProps {
    open: boolean
    sending: boolean
    onClose: () => void
    onSend: (params: { amount: number; message: string }) => void
}

export const RedPacketComposer: React.FC<RedPacketComposerProps> = ({ open, sending, onClose, onSend }) => {
    const { t } = useT()
    const [amountDraft, setAmountDraft] = useState('')
    const [blessingDraft, setBlessingDraft] = useState('')
    const [error, setError] = useState<string | null>(null)
    const amountInputRef = useRef<HTMLInputElement>(null)

    useEffect(() => {
        if (!open) return
        setError(null)
        window.setTimeout(() => {
            amountInputRef.current?.focus()
        }, 0)
    }, [open])

    if (!open) return null

    const handleSend = () => {
        setError(null)
        const amountValue = Number.parseFloat(amountDraft)
        if (!Number.isFinite(amountValue) || amountValue <= 0) {
            setError(t('redPacket.invalidAmount'))
            return
        }
        const blessing = blessingDraft.trim()
        if (!blessing) {
            setError(t('redPacket.greetingRequired'))
            return
        }
        if (blessing.length > 10) {
            setError(t('redPacket.greetingTooLong'))
            return
        }

        const normalizedAmount = Math.round(amountValue * 100) / 100
        onSend({ amount: normalizedAmount, message: blessing })
        setAmountDraft('')
        setBlessingDraft('')
        setError(null)
        onClose()
    }

    return createPortal(
        <div className="rp-compose-overlay">
            <div className="rp-compose-topbar">
                <button type="button" className="rp-compose-back" onClick={onClose} aria-label={t('common.back')}>
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M15.5 5.5a1 1 0 0 1 0 1.4L10.4 12l5.1 5.1a1 1 0 1 1-1.4 1.4l-5.8-5.8a1 1 0 0 1 0-1.4l5.8-5.8a1 1 0 0 1 1.4 0z" />
                    </svg>
                </button>
                <div className="rp-compose-topbar-title">{t('redPacket.sendRedPacket')}</div>
                <div className="rp-compose-topbar-spacer" />
            </div>

            <div className="rp-compose-content">
                <div className="rp-compose-form">
                    <div className="rp-compose-row">
                        <input
                            ref={amountInputRef}
                            className="rp-compose-row-input"
                            type="number"
                            inputMode="decimal"
                            min="0.01"
                            step="0.01"
                            placeholder={t('redPacket.amountPlaceholder')}
                            value={amountDraft}
                            onChange={(e) => setAmountDraft(e.target.value)}
                        />
                        <div className="rp-compose-row-right">
                            ¥
                            {(() => {
                                const value = Number.parseFloat(amountDraft)
                                if (!Number.isFinite(value) || value <= 0) return '0.00'
                                return value.toFixed(2)
                            })()}
                        </div>
                    </div>

                    <div className="rp-compose-row">
                        <input
                            className="rp-compose-row-input"
                            type="text"
                            placeholder={t('redPacket.greetingPlaceholder')}
                            value={blessingDraft}
                            maxLength={10}
                            onChange={(e) => setBlessingDraft(e.target.value)}
                        />
                        <div className="rp-compose-row-right subtle">{blessingDraft.length}/10</div>
                    </div>
                </div>

                <div className="rp-compose-amount-preview">
                    <span className="rp-compose-amount-currency">¥</span>
                    <span className="rp-compose-amount-value">
                        {(() => {
                            const value = Number.parseFloat(amountDraft)
                            if (!Number.isFinite(value) || value <= 0) return '0.00'
                            return value.toFixed(2)
                        })()}
                    </span>
                </div>

                {error && <div className="rp-compose-error">{error}</div>}

                <button type="button" className="rp-compose-send" onClick={handleSend} disabled={sending}>
                    {t('redPacket.sendButton')}
                </button>

                <div className="rp-compose-footnote">{t('redPacket.refundHint')}</div>
            </div>
        </div>,
        document.body
    )
}
