import React, { createContext, useContext, useState, useCallback, ReactNode, useRef } from 'react'
import { createPortal } from 'react-dom'
import { useT } from './I18nContext'
import './ConfirmModal.css'

interface ConfirmOptions {
    title?: string
    message: string
    confirmText?: string
    cancelText?: string
    danger?: boolean
}

interface ConfirmContextType {
    confirm: (options: ConfirmOptions) => Promise<boolean>
}

const ConfirmContext = createContext<ConfirmContextType | undefined>(undefined)

export const useConfirm = () => {
    const context = useContext(ConfirmContext)
    if (!context) {
        throw new Error('useConfirm must be used within a ConfirmProvider')
    }
    return context
}

export const ConfirmProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
    const { t } = useT()
    const [isOpen, setIsOpen] = useState(false)
    const [options, setOptions] = useState<ConfirmOptions>({ message: '' })
    const resolveRef = useRef<(value: boolean) => void>(null)

    const confirm = useCallback((opts: ConfirmOptions) => {
        setOptions(opts)
        setIsOpen(true)
        return new Promise<boolean>((resolve) => {
            // @ts-ignore
            resolveRef.current = resolve
        })
    }, [])

    const handleConfirm = () => {
        setIsOpen(false)
        if (resolveRef.current) {
            resolveRef.current(true)
        }
    }

    const handleCancel = () => {
        setIsOpen(false)
        if (resolveRef.current) {
            resolveRef.current(false)
        }
    }

    return (
        <ConfirmContext.Provider value={{ confirm }}>
            {children}
            {isOpen &&
                createPortal(
                    <div className="confirm-modal-overlay" onClick={handleCancel}>
                        <div className="confirm-modal-card" onClick={(e) => e.stopPropagation()}>
                            <div className="confirm-modal-header">
                                <div className="confirm-modal-title">{options.title || t('common.confirm')}</div>
                            </div>
                            <div className="confirm-modal-body">{options.message}</div>
                            <div className="confirm-modal-footer">
                                <button className="confirm-modal-btn cancel" onClick={handleCancel}>
                                    {options.cancelText || t('common.cancel')}
                                </button>
                                <button
                                    className={`confirm-modal-btn confirm ${options.danger ? 'danger' : ''}`}
                                    onClick={handleConfirm}
                                >
                                    {options.confirmText || t('common.ok')}
                                </button>
                            </div>
                        </div>
                    </div>,
                    document.body
                )}
        </ConfirmContext.Provider>
    )
}
