export type LogLevel = 'INFO' | 'WARN' | 'ERROR' | 'FATAL'

export interface LogField {
    key: string
    value: string
}

/** One parsed line of cornerstone.log. `offset` is the byte offset of the line and its stable id. */
export interface LogEntry {
    offset: number
    time?: string
    level?: LogLevel | string
    source?: string
    event?: string
    message?: string
    fields?: LogField[]
    raw: string
    truncated?: boolean
}

/** Entries are ordered oldest → newest. */
export interface LogPage {
    entries: LogEntry[]
    cursor: number
    next_before: number
    has_more: boolean
    size: number
    reset?: boolean
    gap?: boolean
    file: string
}

export interface LogQuery {
    levels?: LogLevel[]
    search?: string
    excludeEventPrefixes?: string[]
    limit?: number
    before?: number
    after?: number
}
