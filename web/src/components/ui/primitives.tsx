import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Icon, type IconName } from './Icon'

const cx = (...values: Array<string | false | null | undefined>) => values.filter(Boolean).join(' ')

/* ───────── Page ───────── */

export interface PageProps {
    children: ReactNode
    wide?: boolean
    className?: string
    label?: string
}

/** Scrollable page canvas with a centred reading column. */
export function Page({ children, wide, className, label }: PageProps) {
    return (
        <div className={cx('ui-page', className)} aria-label={label}>
            <div className={cx('ui-page__inner', wide && 'ui-page__inner--wide')}>{children}</div>
        </div>
    )
}

export interface PageHeaderProps {
    title: ReactNode
    description?: ReactNode
    actions?: ReactNode
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
    return (
        <header className="ui-page-header">
            <div className="ui-page-header__text">
                <h1 className="ui-page-header__title">{title}</h1>
                {description && <p className="ui-page-header__desc">{description}</p>}
            </div>
            {actions && <div className="ui-page-header__actions">{actions}</div>}
        </header>
    )
}

/* ───────── Grouped list ───────── */

export interface GroupProps {
    title?: ReactNode
    description?: ReactNode
    action?: ReactNode
    footer?: ReactNode
    children: ReactNode
    /** Render children directly instead of inside the bordered list surface. */
    bare?: boolean
    className?: string
    id?: string
}

export function Group({ title, description, action, footer, children, bare, className, id }: GroupProps) {
    return (
        <section className={cx('ui-group', className)} id={id}>
            {(title || action) && (
                <div className="ui-group__head">
                    <div>
                        {title && <h2 className="ui-group__title">{title}</h2>}
                        {description && <p className="ui-group__desc">{description}</p>}
                    </div>
                    {action}
                </div>
            )}
            {bare ? children : <div className="ui-list">{children}</div>}
            {footer && <p className="ui-group__foot">{footer}</p>}
        </section>
    )
}

interface RowContentProps {
    icon?: IconName
    label: ReactNode
    detail?: ReactNode
    value?: ReactNode
    trailing?: ReactNode
    chevron?: boolean
}

function RowContent({ icon, label, detail, value, trailing, chevron }: RowContentProps) {
    return (
        <>
            {icon && (
                <span className="ui-row__icon">
                    <Icon name={icon} />
                </span>
            )}
            <span className="ui-row__main">
                <span className="ui-row__label">{label}</span>
                {detail && <span className="ui-row__detail">{detail}</span>}
            </span>
            {value !== undefined && value !== null && value !== '' && <span className="ui-row__value">{value}</span>}
            {trailing}
            {chevron && <Icon name="chevronRight" className="ui-row__chevron" />}
        </>
    )
}

export type RowProps = RowContentProps &
    Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'value'> & {
        /** Navigate with a router link instead of acting as a button. */
        to?: string
    }

/** A row inside a `Group`. Becomes a button with `onClick`, a link with `to`, otherwise a static row. */
export function Row({ icon, label, detail, value, trailing, chevron, to, onClick, className, ...rest }: RowProps) {
    const classes = cx('ui-row', icon && 'ui-row--has-icon', className)
    const content = (
        <RowContent
            icon={icon}
            label={label}
            detail={detail}
            value={value}
            trailing={trailing}
            chevron={chevron ?? Boolean(onClick || to)}
        />
    )
    if (to) {
        return (
            <Link to={to} className={classes}>
                {content}
            </Link>
        )
    }
    if (onClick) {
        return (
            <button type="button" className={classes} onClick={onClick} {...rest}>
                {content}
            </button>
        )
    }
    return <div className={classes}>{content}</div>
}

/* ───────── Toggle ───────── */

export interface ToggleProps {
    checked: boolean
    onChange: (checked: boolean) => void
    label: string
    disabled?: boolean
}

/** Uses the shared `.toggle-switch` markup so styling stays in one place. */
export function Toggle({ checked, onChange, label, disabled }: ToggleProps) {
    return (
        <label className="toggle-switch">
            <input
                type="checkbox"
                role="switch"
                aria-label={label}
                checked={checked}
                disabled={disabled}
                onChange={(event) => onChange(event.target.checked)}
            />
            <span className="toggle-slider" aria-hidden="true" />
        </label>
    )
}

/* ───────── Badge ───────── */

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

export function Badge({ tone = 'neutral', dot, children }: { tone?: Tone; dot?: boolean; children: ReactNode }) {
    return (
        <span className={cx('ui-badge', tone !== 'neutral' && `ui-badge--${tone}`)}>
            {dot && <span className="ui-dot" aria-hidden="true" />}
            {children}
        </span>
    )
}

/* ───────── Empty state ───────── */

export interface EmptyStateProps {
    icon?: IconName
    title: ReactNode
    description?: ReactNode
    action?: ReactNode
    role?: 'status' | 'alert'
}

export function EmptyState({ icon, title, description, action, role }: EmptyStateProps) {
    return (
        <div className="ui-empty" role={role}>
            {icon && (
                <span className="ui-empty__icon">
                    <Icon name={icon} />
                </span>
            )}
            <span className="ui-empty__title">{title}</span>
            {description && <span className="ui-empty__desc">{description}</span>}
            {action && <div className="ui-empty__actions">{action}</div>}
        </div>
    )
}

/* ───────── Avatar ───────── */

export function Avatar({
    src,
    name,
    size,
    className,
}: {
    src?: string | null
    name: string
    size?: 'sm' | 'lg'
    className?: string
}) {
    return (
        <span className={cx('ui-avatar', size && `ui-avatar--${size}`, className)} aria-hidden="true">
            {src ? <img src={src} alt="" /> : (name.trim().charAt(0) || '?').toUpperCase()}
        </span>
    )
}

export { cx }
