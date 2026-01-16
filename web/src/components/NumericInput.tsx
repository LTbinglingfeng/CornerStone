import { useEffect, useState } from 'react'

type NumericParseMode = 'int' | 'float'

export interface NumericInputProps
    extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type' | 'value' | 'onChange'> {
    value: number | null | undefined
    onValueChange: (value: number) => void
    parseAs?: NumericParseMode
}

const toFiniteNumber = (value: unknown) => {
    const parsed = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : Number.NaN
    return Number.isFinite(parsed) ? parsed : null
}

const isIntermediateNumberText = (raw: string, parseAs: NumericParseMode) => {
    const trimmed = raw.trim()
    if (trimmed === '' || trimmed === '+' || trimmed === '-') return true
    if (parseAs === 'float') {
        if (trimmed === '.' || trimmed === '+.' || trimmed === '-.') return true
        if (trimmed.endsWith('.')) return true
        if (/[eE][+-]?$/.test(trimmed)) return true
    }
    return false
}

const parseNumberText = (raw: string, parseAs: NumericParseMode) => {
    const trimmed = raw.trim()
    if (trimmed === '') return null

    if (parseAs === 'int') {
        const match = /^[+-]?\d+$/.test(trimmed)
        if (!match) return null
        const parsed = Number.parseInt(trimmed, 10)
        return Number.isFinite(parsed) ? parsed : null
    }

    if (isIntermediateNumberText(trimmed, 'float')) return null
    const parsed = Number(trimmed)
    return Number.isFinite(parsed) ? parsed : null
}

const parseNumberTextOnBlur = (raw: string, parseAs: NumericParseMode) => {
    const trimmed = raw.trim()
    if (trimmed === '') return null

    if (parseAs === 'int') {
        const parsed = Number.parseInt(trimmed, 10)
        return Number.isFinite(parsed) ? parsed : null
    }

    const parsed = Number(trimmed)
    return Number.isFinite(parsed) ? parsed : null
}

export const NumericInput: React.FC<NumericInputProps> = ({ value, onValueChange, parseAs = 'float', onBlur, ...rest }) => {
    const [text, setText] = useState(() => (Number.isFinite(value as number) ? String(value) : ''))

    useEffect(() => {
        setText(Number.isFinite(value as number) ? String(value) : '')
    }, [value])

    return (
        <input
            {...rest}
            type="number"
            value={text}
            onChange={(e) => {
                const nextText = e.target.value
                setText(nextText)

                if (isIntermediateNumberText(nextText, parseAs)) return
                const parsed = parseNumberText(nextText, parseAs)
                if (parsed === null) return
                onValueChange(parsed)
            }}
            onBlur={(e) => {
                const nextText = e.target.value
                const minValue = toFiniteNumber(rest.min)
                const maxValue = toFiniteNumber(rest.max)
                const currentValue = toFiniteNumber(value)

                if (nextText.trim() === '') {
                    const fallback = currentValue ?? minValue ?? 0
                    const clamped =
                        minValue !== null && maxValue !== null
                            ? Math.min(Math.max(fallback, minValue), maxValue)
                            : minValue !== null
                              ? Math.max(fallback, minValue)
                              : maxValue !== null
                                ? Math.min(fallback, maxValue)
                                : fallback
                    if (clamped !== currentValue) onValueChange(clamped)
                    setText(String(clamped))
                    onBlur?.(e)
                    return
                }

                const parsed = parseNumberTextOnBlur(nextText, parseAs)
                if (parsed === null) {
                    setText(currentValue !== null ? String(currentValue) : minValue !== null ? String(minValue) : '0')
                    onBlur?.(e)
                    return
                }

                const clamped =
                    minValue !== null && maxValue !== null
                        ? Math.min(Math.max(parsed, minValue), maxValue)
                        : minValue !== null
                          ? Math.max(parsed, minValue)
                          : maxValue !== null
                            ? Math.min(parsed, maxValue)
                            : parsed
                if (clamped !== currentValue) onValueChange(clamped)

                onBlur?.(e)
            }}
        />
    )
}
