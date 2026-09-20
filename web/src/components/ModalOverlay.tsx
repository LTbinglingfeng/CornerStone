import { useLayoutEffect, useRef } from 'react'
import { motion, type HTMLMotionProps } from 'motion/react'
import './ModalOverlay.css'

export type ModalOverlayProps = Omit<HTMLMotionProps<'dialog'>, 'open' | 'onCancel' | 'aria-label'> & {
    'aria-label': string
    onDismiss: () => void
}

/** Keep mounted through AnimatePresence's exit so the background stays inert until removal. */
export function ModalOverlay({ children, className = '', onDismiss, onClick, ...props }: ModalOverlayProps) {
    const dialogRef = useRef<HTMLDialogElement>(null)

    useLayoutEffect(() => {
        const dialog = dialogRef.current
        if (!dialog) return
        const previousFocus = document.activeElement
        dialog.showModal()
        return () => {
            dialog.close()
            if (previousFocus instanceof HTMLElement && previousFocus.isConnected) {
                previousFocus.focus({ preventScroll: true })
            }
        }
    }, [])

    return (
        <motion.dialog
            {...props}
            ref={dialogRef}
            className={`native-modal-overlay ${className}`}
            onCancel={(event) => {
                // React owns dismissal (including saving guards and exit animations).
                event.preventDefault()
                onDismiss()
            }}
            onClick={(event) => {
                onClick?.(event)
                if (!event.defaultPrevented && event.target === event.currentTarget) onDismiss()
            }}
        >
            {children}
        </motion.dialog>
    )
}
