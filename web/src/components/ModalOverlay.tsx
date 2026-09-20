import { useLayoutEffect, useRef } from 'react'
import { motion, type HTMLMotionProps } from 'motion/react'
import './ModalOverlay.css'

export type ModalOverlayProps = Omit<HTMLMotionProps<'dialog'>, 'open' | 'onCancel' | 'aria-label'> & {
    'aria-label': string
    onDismiss: () => void
}

/** Keep mounted through AnimatePresence's exit so the background stays inert until removal. */
export function ModalOverlay({ children, className = '', onDismiss, onClick, onKeyDown, ...props }: ModalOverlayProps) {
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
            onKeyDown={(event) => {
                onKeyDown?.(event)
                if (event.defaultPrevented || event.key !== 'Tab') return
                const controls = Array.from(
                    event.currentTarget.querySelectorAll<HTMLElement>(
                        'a[href], button:not(:disabled), input:not(:disabled):not([type="hidden"]), select:not(:disabled), textarea:not(:disabled), [tabindex], summary'
                    )
                ).filter(
                    (element) =>
                        element.tabIndex >= 0 &&
                        element.getClientRects().length > 0 &&
                        getComputedStyle(element).visibility !== 'hidden'
                )
                const first = controls[0]
                const last = controls[controls.length - 1]
                // Native modal inertness blocks background UI; explicit wrapping also
                // avoids handing repeated Tab navigation to the browser toolbar.
                if (!first) {
                    event.preventDefault()
                    event.currentTarget.focus()
                } else if (
                    event.shiftKey &&
                    (document.activeElement === first || document.activeElement === event.currentTarget)
                ) {
                    event.preventDefault()
                    last.focus()
                } else if (!event.shiftKey && document.activeElement === last) {
                    event.preventDefault()
                    first.focus()
                }
            }}
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
