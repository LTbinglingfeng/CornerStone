import { translate } from '../i18n'

export interface Memory {
    id: string
    subject: 'user' | 'self'
    category: string
    content: string
    strength: number
    current_strength: number
    last_seen: string
    seen_count: number
    created_at: string
    pinned: boolean
}

export interface MemorySettings {
    memory_provider_id: string
    memory_enabled: boolean
}

export const userCategories = ['identity', 'relation', 'fact', 'preference', 'event', 'emotion']

export const selfCategories = ['promise', 'plan', 'statement', 'opinion']

export const getCategoryLabel = (category: string): string => {
    const key = `memoryCategory.${category}` as const
    const translated = translate(key)
    return translated === key ? category : translated
}
