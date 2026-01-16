import { useEffect, useRef } from 'react'

interface UseKeyboardOffsetOptions {
  containerRef: React.RefObject<HTMLElement | null>
  messageListRef?: React.RefObject<HTMLElement | null>
}

export function useKeyboardOffset(options: UseKeyboardOffsetOptions): void {
  const { containerRef, messageListRef } = options
  const keyboardOffsetRef = useRef(0)
  const keyboardOffsetRafRef = useRef<number | null>(null)

  useEffect(() => {
    const target = containerRef.current
    if (!target) return

    const applyOffset = (nextOffset: number) => {
      const offset = Math.max(0, Math.round(nextOffset))
      if (offset === keyboardOffsetRef.current) return
      keyboardOffsetRef.current = offset
      target.style.setProperty('--chat-keyboard-offset', `${offset}px`)
      if (offset > 0) {
        window.setTimeout(() => {
          const list = messageListRef?.current
          if (!list) return
          list.scrollTop = list.scrollHeight
        }, 0)
      }
    }

    const update = () => {
      if (keyboardOffsetRafRef.current !== null) return
      keyboardOffsetRafRef.current = window.requestAnimationFrame(() => {
        keyboardOffsetRafRef.current = null
        const viewport = window.visualViewport
        if (!viewport) {
          applyOffset(0)
          return
        }
        applyOffset(window.innerHeight - viewport.height - viewport.offsetTop)
      })
    }

    const viewport = window.visualViewport
    update()
    window.addEventListener('resize', update)
    viewport?.addEventListener('resize', update)
    viewport?.addEventListener('scroll', update)

    return () => {
      window.removeEventListener('resize', update)
      viewport?.removeEventListener('resize', update)
      viewport?.removeEventListener('scroll', update)
      if (keyboardOffsetRafRef.current !== null) {
        window.cancelAnimationFrame(keyboardOffsetRafRef.current)
        keyboardOffsetRafRef.current = null
      }
      keyboardOffsetRef.current = 0
      target.style.setProperty('--chat-keyboard-offset', '0px')
    }
  }, [containerRef, messageListRef])
}

