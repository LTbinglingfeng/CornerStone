import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import './ContextMenu.css'

export interface MenuItem {
    label: string
    onClick: () => void
    danger?: boolean
}

interface ContextMenuProps {
    items: MenuItem[]
    position: { x: number; y: number }
    onClose: () => void
}

const VIEWPORT_GUTTER = 8

/** Floating menu anchored at the pointer; flips to stay on screen and scales from the anchor corner. */
const ContextMenu: React.FC<ContextMenuProps> = ({ items, position, onClose }) => {
    const menuRef = useRef<HTMLDivElement>(null)
    const [placement, setPlacement] = useState<{ left: number; top: number; origin: string } | null>(null)

    useEffect(() => {
        const handleClickOutside = (e: MouseEvent | TouchEvent) => {
            if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
                onClose()
            }
        }
        const handleKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                e.preventDefault()
                onClose()
            }
        }

        document.addEventListener('mousedown', handleClickOutside)
        document.addEventListener('touchstart', handleClickOutside)
        document.addEventListener('keydown', handleKeyDown)

        return () => {
            document.removeEventListener('mousedown', handleClickOutside)
            document.removeEventListener('touchstart', handleClickOutside)
            document.removeEventListener('keydown', handleKeyDown)
        }
    }, [onClose])

    // Measure once mounted so the menu never overflows the viewport.
    useLayoutEffect(() => {
        const menu = menuRef.current
        if (!menu) return
        const { width, height } = menu.getBoundingClientRect()
        const flipX = position.x + width > window.innerWidth - VIEWPORT_GUTTER
        const flipY = position.y + height > window.innerHeight - VIEWPORT_GUTTER
        const left = Math.max(VIEWPORT_GUTTER, flipX ? position.x - width : position.x)
        const top = Math.max(VIEWPORT_GUTTER, flipY ? position.y - height : position.y)
        setPlacement({ left, top, origin: `${flipY ? 'bottom' : 'top'} ${flipX ? 'right' : 'left'}` })
        // Focus the menu itself (not the first item) so pointer users don't see a stuck highlight;
        // arrow keys then move into the items.
        menu.focus({ preventScroll: true })
    }, [position.x, position.y, items.length])

    const moveFocus = (step: number) => {
        const buttons = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') || [])
        if (buttons.length === 0) return
        const current = buttons.indexOf(document.activeElement as HTMLButtonElement)
        buttons[(current + step + buttons.length) % buttons.length].focus()
    }

    // Portal to body so ancestor transforms never offset fixed positioning.
    return createPortal(
        <div className="context-menu-overlay">
            <div
                ref={menuRef}
                className="context-menu"
                role="menu"
                tabIndex={-1}
                style={{
                    left: placement?.left ?? position.x,
                    top: placement?.top ?? position.y,
                    transformOrigin: placement?.origin,
                }}
                onKeyDown={(event) => {
                    if (event.key === 'ArrowDown') {
                        event.preventDefault()
                        moveFocus(1)
                    } else if (event.key === 'ArrowUp') {
                        event.preventDefault()
                        moveFocus(-1)
                    }
                }}
            >
                {items.map((item, index) => (
                    <button
                        key={index}
                        type="button"
                        role="menuitem"
                        className={`context-menu-item${item.danger ? ' danger' : ''}`}
                        onClick={() => {
                            item.onClick()
                            onClose()
                        }}
                    >
                        {item.label}
                    </button>
                ))}
            </div>
        </div>,
        document.body
    )
}

export default ContextMenu
