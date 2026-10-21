import type { ReactNode } from 'react'
import { cx } from './ui'
import './Settings.css'

export interface SettingsFieldProps {
    label: ReactNode
    hint?: ReactNode
    /** Put the control under the label (textareas, wide inputs). */
    stacked?: boolean
    children: ReactNode
    className?: string
}

/**
 * A labelled control inside a `Group` list — macOS System Settings style:
 * label (and optional hint) on the left, control on the right; stacks on narrow screens.
 */
export function SettingsField({ label, hint, stacked, children, className }: SettingsFieldProps) {
    return (
        <div className={cx('settings-field', stacked && 'settings-field--stacked', className)}>
            <div className="settings-field__text">
                <span className="settings-field__label">{label}</span>
                {hint && <span className="settings-field__hint">{hint}</span>}
            </div>
            <div className="settings-field__control">{children}</div>
        </div>
    )
}

export default SettingsField
