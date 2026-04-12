import type { ChatMessage } from '../../types/chat'
import { translate } from '../../i18n'
import { resolveAssistantMessageSplitToken } from '../../utils/assistantMessageSplit'
import {
    QUOTE_PREFIX_CANDIDATES,
    RECALLED_MESSAGE_SUFFIX_CANDIDATES,
    getQuotedMessagePrefix,
} from './constants'

export const isRecalledMessage = (message: ChatMessage): boolean => {
    if (message.role !== 'user') return false
    const trimmed = message.content.trimEnd()
    return RECALLED_MESSAGE_SUFFIX_CANDIDATES.some((suffix) => trimmed.endsWith(suffix))
}

export const parseQuotedMessageContent = (content: string): { quoteLine: string; text: string } | null => {
    if (!content) return null
    for (const prefix of QUOTE_PREFIX_CANDIDATES) {
        if (!content.startsWith(prefix)) continue
        const payload = content.slice(prefix.length).trimStart()
        const newlineIndex = payload.indexOf('\n')
        if (newlineIndex === -1) {
            return { quoteLine: payload.trim(), text: '' }
        }
        return {
            quoteLine: payload.slice(0, newlineIndex).trim(),
            text: payload.slice(newlineIndex + 1),
        }
    }
    return null
}

export const buildQuotedOutgoingContent = (quoteLine: string, text: string): string => {
    const header = `${getQuotedMessagePrefix()} ${quoteLine}`
    if (text.trim() === '') return header
    return `${header}\n${text}`
}

export const normalizeAssistantContent = (content: string): string => {
    if (!content) return ''
    const withoutBlocks = content.replace(/<think[^>]*>[\s\S]*?<\/think\s*>/gi, '')
    const lower = withoutBlocks.toLowerCase()
    const openIndex = lower.indexOf('<think')
    if (openIndex !== -1) {
        return withoutBlocks.slice(0, openIndex)
    }
    return withoutBlocks.replace(/<\/think\s*>/gi, '')
}

export const splitAssistantMessageContent = (content: string, splitToken?: string | null): string[] => {
    const normalized = normalizeAssistantContent(content)
    if (!normalized) return []
    const resolvedSplitToken = resolveAssistantMessageSplitToken(splitToken)
    if (!resolvedSplitToken || !normalized.includes(resolvedSplitToken)) {
        return normalized.trim() ? [normalized] : []
    }

    return normalized
        .split(resolvedSplitToken)
        .map((part) => part.trim())
        .filter((part) => part !== '')
}

export const buildQuoteLineFromMessage = (
    message: ChatMessage,
    getRoleDisplayName: (role: string) => string
): string => {
    const name = getRoleDisplayName(message.role)
    const rawContent = message.role === 'assistant' ? normalizeAssistantContent(message.content) : message.content
    const parsed = parseQuotedMessageContent(rawContent)
    let quoteText = (parsed ? parsed.text : rawContent).trim()
    if (!quoteText && message.image_paths && message.image_paths.length > 0) {
        quoteText = translate('chat.imageText')
    }
    quoteText = quoteText.replace(/\s+/g, ' ').trim()
    const maxLen = 80
    if (quoteText.length > maxLen) {
        quoteText = quoteText.slice(0, maxLen) + '...'
    }
    return `${name}: ${quoteText || '...'}`
}

export const buildSelectableText = (message: ChatMessage): string => {
    const rawContent = message.role === 'assistant' ? normalizeAssistantContent(message.content) : message.content
    const parsed = parseQuotedMessageContent(rawContent)
    if (!parsed) return rawContent
    const quoteLine = parsed.quoteLine.trim()
    const text = parsed.text
    if (quoteLine && text.trim() !== '') {
        return `${quoteLine}\n${text}`
    }
    return quoteLine || text
}
