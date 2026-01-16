import { useState, useEffect, useRef } from 'react'
import type { SelectOption } from './constants'

interface CustomSelectProps {
    value: string
    options: SelectOption[]
    onChange: (value: string) => void
    ariaLabel?: string
    disabled?: boolean
}

const CustomSelect: React.FC<CustomSelectProps> = ({
    value,
    options,
    onChange,
    ariaLabel,
    disabled = false
}) => {
    const [open, setOpen] = useState(false)
    const wrapperRef = useRef<HTMLDivElement>(null)
    const selectedOption = options.find((option) => option.value === value)
    const displayLabel = selectedOption?.label || value || options[0]?.label || '请选择'

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
        <div className={`modal-select-ui${open ? ' open' : ''}`} ref={wrapperRef}>
            <button
                type="button"
                className="modal-input modal-select-trigger"
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-disabled={disabled}
                onClick={() => {
                    if (!disabled) setOpen((prev) => !prev)
                }}
            >
                <span className="modal-select-text">{displayLabel}</span>
                <svg className="modal-select-icon" viewBox="0 0 24 24">
                    <path d="M7 10l5 5 5-5z" />
                </svg>
            </button>
            {open && (
                <div className="modal-select-menu" role="listbox" aria-label={ariaLabel}>
                    {options.map((option) => {
                        const isActive = option.value === value
                        return (
                            <button
                                type="button"
                                key={option.value}
                                className={`modal-select-option${isActive ? ' active' : ''}`}
                                role="option"
                                aria-selected={isActive}
                                onClick={() => {
                                    onChange(option.value)
                                    setOpen(false)
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
