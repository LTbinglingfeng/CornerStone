import { useState, useEffect, useLayoutEffect, useId, useRef } from 'react'
import { useT } from '../../contexts/I18nContext'
import type { SelectOption } from './constants'

interface CustomSelectProps {
    value: string
    options: SelectOption[]
    onChange: (value: string) => void
    ariaLabel?: string
    disabled?: boolean
}

const CustomSelect: React.FC<CustomSelectProps> = ({ value, options, onChange, ariaLabel, disabled = false }) => {
    const { t } = useT()
    const [open, setOpen] = useState(false)
    const listboxId = useId()
    const triggerRef = useRef<HTMLButtonElement>(null)
    const optionRefs = useRef<(HTMLButtonElement | null)[]>([])
    const activeIndex = useRef(-1)
    const pendingFocus = useRef<number | null>(null)
    const wrapperRef = useRef<HTMLDivElement>(null)
    const selectedOption = options.find((option) => option.value === value)
    const displayLabel = selectedOption?.label || value || options[0]?.label || t('common.select')

    const closeAndRestoreFocus = () => {
        setOpen(false)
        triggerRef.current?.focus({ preventScroll: true })
    }

    const focusOption = (index: number) => {
        activeIndex.current = index
        optionRefs.current[index]?.focus({ preventScroll: true })
        optionRefs.current[index]?.scrollIntoView({ block: 'nearest' })
    }

    useLayoutEffect(() => {
        if (open && pendingFocus.current !== null) {
            focusOption(pendingFocus.current)
            pendingFocus.current = null
        }
    }, [open])

    useEffect(() => {
        if (!open) return
        const handleClickOutside = (event: MouseEvent) => {
            if (!wrapperRef.current) return
            if (!wrapperRef.current.contains(event.target as Node)) {
                setOpen(false)
            }
        }
        document.addEventListener('mousedown', handleClickOutside)
        return () => document.removeEventListener('mousedown', handleClickOutside)
    }, [open])

    useEffect(() => {
        if (disabled && open) {
            setOpen(false)
        }
    }, [disabled, open])

    return (
        <div
            className={`modal-select-ui${open ? ' open' : ''}`}
            ref={wrapperRef}
            onBlur={(event) => {
                if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false)
            }}
            onKeyDown={(event) => {
                if (disabled) return
                if (event.key === 'Escape' && open) {
                    event.preventDefault()
                    event.stopPropagation()
                    closeAndRestoreFocus()
                    return
                }
                const navigationKeys = ['ArrowDown', 'ArrowUp', 'Home', 'End']
                const selectKey = event.key === 'Enter' || event.key === ' '
                if (!navigationKeys.includes(event.key) && !selectKey) return
                event.preventDefault()
                event.stopPropagation()
                if (!options.length) return

                if (!open) {
                    const selectedIndex = options.findIndex((option) => option.value === value)
                    const index =
                        event.key === 'Home'
                            ? 0
                            : event.key === 'End'
                              ? options.length - 1
                              : selectedIndex >= 0
                                ? selectedIndex
                                : event.key === 'ArrowUp'
                                  ? options.length - 1
                                  : 0
                    pendingFocus.current = index
                    setOpen(true)
                    return
                }
                if (selectKey) {
                    const option = options[activeIndex.current]
                    if (option) {
                        onChange(option.value)
                        closeAndRestoreFocus()
                    }
                    return
                }
                const index =
                    event.key === 'Home'
                        ? 0
                        : event.key === 'End'
                          ? options.length - 1
                          : event.key === 'ArrowDown'
                            ? Math.min(activeIndex.current + 1, options.length - 1)
                            : Math.max(activeIndex.current - 1, 0)
                focusOption(index)
            }}
        >
            <button
                ref={triggerRef}
                type="button"
                disabled={disabled}
                aria-label={ariaLabel}
                aria-controls={open ? listboxId : undefined}
                className="modal-input modal-select-trigger"
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-disabled={disabled}
                onClick={() => {
                    if (!disabled) {
                        triggerRef.current?.focus({ preventScroll: true })
                        activeIndex.current = Math.max(
                            0,
                            options.findIndex((option) => option.value === value)
                        )
                        setOpen((prev) => !prev)
                    }
                }}
            >
                <span className="modal-select-text">{displayLabel}</span>
                <svg className="modal-select-icon" viewBox="0 0 24 24">
                    <path d="M7 10l5 5 5-5z" />
                </svg>
            </button>
            {open && (
                <div id={listboxId} className="modal-select-menu" role="listbox" aria-label={ariaLabel || displayLabel}>
                    {options.map((option, index) => {
                        const isActive = option.value === value
                        return (
                            <button
                                ref={(element) => {
                                    optionRefs.current[index] = element
                                }}
                                disabled={disabled}
                                tabIndex={-1}
                                onFocus={() => {
                                    activeIndex.current = index
                                }}
                                type="button"
                                key={option.value}
                                className={`modal-select-option${isActive ? ' active' : ''}`}
                                role="option"
                                aria-selected={isActive}
                                onClick={() => {
                                    onChange(option.value)
                                    closeAndRestoreFocus()
                                }}
                            >
                                <span>{option.label}</span>
                                {isActive && (
                                    <svg className="modal-select-check" viewBox="0 0 24 24">
                                        <path d="M9 16.17l-3.88-3.88L4 13.41 9 18.41 20 7.41 18.59 6l-9.59 10.17z" />
                                    </svg>
                                )}
                            </button>
                        )
                    })}
                </div>
            )}
        </div>
    )
}

export default CustomSelect
