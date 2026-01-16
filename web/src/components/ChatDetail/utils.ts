import type { ChatMessage } from '../../types/chat'
import {
  ASSISTANT_MESSAGE_SPLIT_TOKEN,
  QUOTE_PREFIX_CANDIDATES,
  RECALLED_MESSAGE_SUFFIX,
} from './constants'

export const isRecalledMessage = (message: ChatMessage): boolean => {
  if (message.role !== 'user') return false
  return message.content.trimEnd().endsWith(RECALLED_MESSAGE_SUFFIX)
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
  const header = `引用的信息: ${quoteLine}`
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

export const splitAssistantMessageContent = (content: string): string[] => {
  const normalized = normalizeAssistantContent(content)
  if (!normalized) return []
  if (!normalized.includes(ASSISTANT_MESSAGE_SPLIT_TOKEN)) {
    return normalized.trim() ? [normalized] : []
  }

  return normalized
    .split(ASSISTANT_MESSAGE_SPLIT_TOKEN)
    .map(part => part.trim())
    .filter(part => part !== '')
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
    quoteText = '图片'
  }
  quoteText = quoteText.replace(/\s+/g, ' ').trim()
  const maxLen = 80
  if (quoteText.length > maxLen) {
    quoteText = quoteText.slice(0, maxLen) + '...'
  }
  return `${name}：${quoteText || '...'}`
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

