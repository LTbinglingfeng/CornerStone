import { useT } from '../../../contexts/I18nContext'
import { TOOL_CONTROL_DEFINITIONS } from '../../../constants/toolControls'
import type { ToolCall } from '../../../types/chat'
import { Icon } from '../../ui'

/** Large results (e.g. web search pages) are clipped so one card cannot freeze the list. */
const MAX_DETAIL_CHARS = 20000

type ToolStatus = 'running' | 'ok' | 'error' | 'missing'

interface ParsedResult {
    status: ToolStatus
    error: string
    body: string
}

function prettyJSON(raw: string): string {
    const trimmed = raw.trim()
    if (trimmed === '') return ''
    try {
        return JSON.stringify(JSON.parse(trimmed), null, 2)
    } catch {
        return raw
    }
}

function clip(text: string): { text: string; clipped: boolean } {
    if (text.length <= MAX_DETAIL_CHARS) return { text, clipped: false }
    return { text: text.slice(0, MAX_DETAIL_CHARS), clipped: true }
}

function parseResult(result: string | undefined, isRunning: boolean): ParsedResult {
    if (result === undefined) {
        return { status: isRunning ? 'running' : 'missing', error: '', body: '' }
    }
    try {
        const payload = JSON.parse(result) as { ok?: unknown; data?: unknown; error?: unknown }
        if (payload && typeof payload === 'object' && typeof payload.ok === 'boolean') {
            const error = typeof payload.error === 'string' ? payload.error : ''
            const body =
                payload.data === undefined || payload.data === null ? '' : JSON.stringify(payload.data, null, 2)
            return { status: payload.ok ? 'ok' : 'error', error, body }
        }
    } catch {
        // Not the structured envelope; show the raw content.
    }
    return { status: 'ok', error: '', body: prettyJSON(result) }
}

interface ToolCallCardProps {
    toolCall: ToolCall
    result?: string
    isRunning: boolean
}

export const ToolCallCard: React.FC<ToolCallCardProps> = ({ toolCall, result, isRunning }) => {
    const { t } = useT()
    const name = toolCall.function.name || 'unknown'
    const definition = TOOL_CONTROL_DEFINITIONS.find((tool) => tool.key === name)
    const label = definition ? t(definition.titleKey) : name
    const parsed = parseResult(result, isRunning)
    const args = clip(prettyJSON(toolCall.function.arguments || ''))
    const body = clip(parsed.body)

    const statusLabel = {
        running: t('chat.toolCallRunning'),
        ok: t('chat.toolCallDone'),
        error: t('chat.toolCallFailed'),
        missing: t('chat.toolCallNoResult'),
    }[parsed.status]

    return (
        <details className={`tool-call-card is-${parsed.status}`}>
            <summary className="tool-call-summary">
                <Icon name="wrench" size={14} className="tool-call-icon" />
                <span className="tool-call-name">{label}</span>
                <span className="tool-call-status">
                    {parsed.status === 'running' && <span className="ui-spinner" aria-hidden="true" />}
                    {statusLabel}
                </span>
                <Icon name="chevronDown" size={14} className="tool-call-chevron" />
            </summary>
            <div className="tool-call-body">
                {definition && <code className="tool-call-raw-name">{name}</code>}
                <div className="tool-call-section">
                    <div className="tool-call-section-title">{t('chat.toolCallArguments')}</div>
                    <pre className="tool-call-pre">{args.text || '{}'}</pre>
                    {args.clipped && <div className="tool-call-clipped">{t('chat.toolCallClipped')}</div>}
                </div>
                {parsed.error && (
                    <div className="tool-call-section">
                        <div className="tool-call-section-title">{t('chat.toolCallError')}</div>
                        <pre className="tool-call-pre is-error">{parsed.error}</pre>
                    </div>
                )}
                {body.text && (
                    <div className="tool-call-section">
                        <div className="tool-call-section-title">{t('chat.toolCallResult')}</div>
                        <pre className="tool-call-pre">{body.text}</pre>
                        {body.clipped && <div className="tool-call-clipped">{t('chat.toolCallClipped')}</div>}
                    </div>
                )}
            </div>
        </details>
    )
}
