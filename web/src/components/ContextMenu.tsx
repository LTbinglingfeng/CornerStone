import { useEffect, useRef } from 'react'
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

const ContextMenu: React.FC<ContextMenuProps> = ({ items, position, onClose }) => {
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent | TouchEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose()
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    document.addEventListener('touchstart', handleClickOutside)

    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      document.removeEventListener('touchstart', handleClickOutside)
    }
  }, [onClose])

  // 调整菜单位置，确保不超出屏幕
  const adjustedPosition = {
    x: Math.min(position.x, window.innerWidth - 160),
    y: Math.min(position.y, window.innerHeight - items.length * 44 - 16),
  }

  // 使用 Portal 将菜单渲染到 body，避免 transform 导致的 fixed 定位问题
  return createPortal(
    <div className="context-menu-overlay">
      <div
        ref={menuRef}
        className="context-menu"
        style={{
          left: adjustedPosition.x,
          top: adjustedPosition.y,
        }}
      >
        {items.map((item, index) => (
          <div
            key={index}
            className={`context-menu-item ${item.danger ? 'danger' : ''}`}
            onClick={() => {
              item.onClick()
              onClose()
            }}
          >
            {item.label}
          </div>
        ))}
      </div>
    </div>,
    document.body
  )
}

export default ContextMenu
