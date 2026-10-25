import React, { createContext, useContext, useState, useCallback, useLayoutEffect, useRef, ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Icon, type IconName } from '../components/ui'
import './Toast.css'

type ToastType = 'success' | 'error' | 'info'

interface ToastMessage {
    id: string
    message: string
    type: ToastType
    leaving: boolean
}

interface ToastContextType {
    showToast: (message: string, type?: ToastType) => void
}

const ToastContext = createContext<ToastContextType | undefined>(undefined)

const TOAST_VISIBLE_MS = 2850
const TOAST_EXIT_MS = 150

const toastIcons: Record<ToastType, IconName> = {
    success: 'check',
    error: 'alert',
    info: 'info',
}

export const useToast = () => {
    const context = useContext(ToastContext)
    if (!context) {
        throw new Error('useToast must be used within a ToastProvider')
    }
    return context
}

export const ToastProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
    const [toasts, setToasts] = useState<ToastMessage[]>([])
    const containerRef = useRef<HTMLDivElement>(null)
    const toastCount = toasts.length

    // Native modal dialogs (showModal) live in the browser top layer and cover any z-index.
    // Promote the toast container into the top layer as a manual popover so it stays visible
    // above open dialogs; re-show on each new toast so it stacks above a dialog opened later.
    useLayoutEffect(() => {
        const container = containerRef.current
        if (!container || typeof container.showPopover !== 'function') return
        const isOpen = container.matches(':popover-open')
        if (toastCount === 0) {
            if (isOpen) container.hidePopover()
            return
        }
        if (isOpen && document.querySelector('dialog:modal')) container.hidePopover()
        if (!container.matches(':popover-open')) container.showPopover()
    }, [toastCount])

    const showToast = useCallback((message: string, type: ToastType = 'info') => {
        const id = Date.now().toString() + Math.random().toString(36).substring(2)
        setToasts((prev) => [...prev, { id, message, type, leaving: false }])

        setTimeout(() => {
            setToasts((prev) => prev.map((t) => (t.id === id ? { ...t, leaving: true } : t)))
        }, TOAST_VISIBLE_MS)
        setTimeout(() => {
            setToasts((prev) => prev.filter((t) => t.id !== id))
        }, TOAST_VISIBLE_MS + TOAST_EXIT_MS)
    }, [])

    return (
        <ToastContext.Provider value={{ showToast }}>
            {children}
            {createPortal(
                <div ref={containerRef} className="toast-container" popover="manual" aria-live="polite">
                    {toasts.map((toast) => (
                        <div
                            key={toast.id}
                            className={`toast toast-${toast.type}${toast.leaving ? ' toast--leaving' : ''}`}
                            role={toast.type === 'error' ? 'alert' : 'status'}
                        >
                            <Icon name={toastIcons[toast.type]} size={16} strokeWidth={2} className="toast-icon" />
                            <span className="toast-message">{toast.message}</span>
                        </div>
                    ))}
                </div>,
                document.body
            )}
        </ToastContext.Provider>
    )
}
