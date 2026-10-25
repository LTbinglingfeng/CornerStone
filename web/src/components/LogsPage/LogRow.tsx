import { memo, type KeyboardEvent, type ReactNode } from 'react'
import { useT } from '../../contexts/I18nContext'
import type { LogEntry } from '../../types/logs'
import { Icon, cx } from '../ui'
import { fieldTone, findField, formatClock, formatFieldValue, formatDate, levelKey, parseEntryTime } from './logFormat'

import { localizeLogEvent, localizeLogField, localizeLogValue } from './logLocalization'

const SUMMARY_FIELD_LIMIT = 6

/** Case-insensitive highlight; skipped when lower-casing changes string length (rare Unicode cases). */
export function Highlight({ text, query }: { text: string; query: string }) {
    if (!query) return <>{text}</>
    const haystack = text.toLowerCase()
    if (haystack.length !== text.length) return <>{text}</>
    const parts: ReactNode[] = []
    let from = 0
    let index = haystack.indexOf(query, from)
    while (index >= 0 && parts.length < 200) {
        if (index > from) parts.push(text.slice(from, index))
        parts.push(<mark key={index}>{text.slice(index, index + query.length)}</mark>)
        from = index + query.length
        index = haystack.indexOf(query, from)
    }
    if (parts.length === 0) return <>{text}</>
    parts.push(text.slice(from))
    return <>{parts}</>
}

export interface LogRowProps {
    entry: LogEntry
    expanded: boolean
    fresh: boolean
    query: string
    onToggle: (offset: number) => void
    onCopy: (text: string) => void
    onFilterRequest: (requestId: string) => void
}

function LogRowImpl({ entry, expanded, fresh, query, onToggle, onCopy, onFilterRequest }: LogRowProps) {
    const { t, locale } = useT()
    const level = levelKey(entry)
    const date = parseEntryTime(entry)
    const fields = entry.fields || []
    const title = entry.event
        ? localizeLogEvent(entry.event, locale)
        : entry.message || (fields.length === 0 ? entry.raw : '')
    const hiddenCount = Math.max(0, fields.length - SUMMARY_FIELD_LIMIT)
    const requestId = findField(entry, 'request')
    const detailsId = `log-${entry.offset}-details`

    const toggle = () => {
        // Selecting text to copy it must not collapse the row under the cursor.
        if (window.getSelection()?.toString()) return
        onToggle(entry.offset)
    }
    const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.target !== event.currentTarget) return
        if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            onToggle(entry.offset)
        }
    }

    return (
        <li
            data-offset={entry.offset}
            className={cx(
                'logs-row',
                `logs-row--${level.toLowerCase()}`,
                expanded && 'is-expanded',
                fresh && 'is-fresh'
            )}
        >
            <div
                className="logs-row__summary"
                role="button"
                tabIndex={0}
                aria-expanded={expanded}
                aria-controls={expanded ? detailsId : undefined}
                aria-label={`${t(`logs.levels.${level}`)} ${formatClock(entry, date)} ${title}`.trim()}
                onClick={toggle}
                onKeyDown={onKeyDown}
            >
                <time className="logs-row__time" dateTime={entry.time}>
                    {formatClock(entry, date) || '—'}
                </time>
                <span className="logs-level" aria-hidden="true">
                    {t(`logs.levels.${level}`)}
                </span>
                <span className="logs-row__body">
                    {title && (
                        <span
                            title={entry.event}
                            className={cx('logs-row__title', entry.event && 'logs-row__title--event')}
                        >
                            <Highlight text={title} query={query} />
                        </span>
                    )}
                    {fields.slice(0, SUMMARY_FIELD_LIMIT).map((field, index) => (
                        <span
                            className={cx('logs-chip', `logs-chip--${fieldTone(field)}`)}
                            key={`${field.key}-${index}`}
                        >
                            <span className="logs-chip__key" title={field.key}>
                                {localizeLogField(field.key, locale)}
                            </span>
                            <span className="logs-chip__value" title={field.value}>
                                <Highlight
                                    text={formatFieldValue({ ...field, value: localizeLogValue(field, locale) }, true)}
                                    query={query}
                                />
                            </span>
                        </span>
                    ))}
                    {hiddenCount > 0 && <span className="logs-chip logs-chip--more">+{hiddenCount}</span>}
                </span>
                {entry.source && <span className="logs-row__source">{entry.source}</span>}
                <Icon name="chevronDown" size={14} className="logs-row__chevron" />
            </div>
            {expanded && (
                <div className="logs-row__details" id={detailsId}>
                    <dl className="logs-details__meta">
                        {date && (
                            <div>
                                <dt>{t('logs.time')}</dt>
                                <dd>
                                    {formatDate(date, locale === 'zh' ? 'zh-CN' : 'en-US')} {formatClock(entry, date)}
                                </dd>
                            </div>
                        )}
                        {entry.source && (
                            <div>
                                <dt>{t('logs.source')}</dt>
                                <dd>{entry.source}</dd>
                            </div>
                        )}
                    </dl>
                    {fields.length > 0 && (
                        <dl className="logs-details__fields" aria-label={t('logs.fields')}>
                            {fields.map((field, index) => (
                                <div key={`${field.key}-${index}`}>
                                    <dt title={field.key}>{localizeLogField(field.key, locale)}</dt>
                                    <dd className={`logs-tone--${fieldTone(field)}`} title={field.value}>
                                        <Highlight
                                            text={field.value === '' ? '""' : localizeLogValue(field, locale)}
                                            query={query}
                                        />
                                        {formatFieldValue(field) !== field.value && field.value !== '' && (
                                            <span className="logs-details__hint">{formatFieldValue(field)}</span>
                                        )}
                                    </dd>
                                </div>
                            ))}
                        </dl>
                    )}
                    <div className="logs-details__raw">
                        <div className="logs-details__raw-head">
                            <span>{t('logs.raw')}</span>
                            <div className="logs-details__actions">
                                {requestId && (
                                    <button
                                        type="button"
                                        className="ui-btn ui-btn--sm ui-btn--ghost"
                                        onClick={() => onFilterRequest(requestId)}
                                    >
                                        <Icon name="search" />
                                        {t('logs.sameRequest')}
                                    </button>
                                )}
                                <button
                                    type="button"
                                    className="ui-btn ui-btn--sm ui-btn--ghost"
                                    onClick={() => onCopy(entry.raw)}
                                >
                                    <Icon name="copy" />
                                    {t('logs.copyRaw')}
                                </button>
                            </div>
                        </div>
                        <pre>{entry.raw}</pre>
                        {entry.truncated && <p className="logs-details__note">{t('logs.truncated')}</p>}
                    </div>
                </div>
            )}
        </li>
    )
}

export const LogRow = memo(LogRowImpl)
