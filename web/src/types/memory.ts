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

export const categoryLabels: Record<string, string> = {
    identity: '身份',
    relation: '关系',
    fact: '事实',
    preference: '偏好',
    event: '事件',
    emotion: '情绪',
    promise: '承诺',
    plan: '约定',
    statement: '自述',
    opinion: '观点',
}

export const userCategories = ['identity', 'relation', 'fact', 'preference', 'event', 'emotion']

export const selfCategories = ['promise', 'plan', 'statement', 'opinion']
