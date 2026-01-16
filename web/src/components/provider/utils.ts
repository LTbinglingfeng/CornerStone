export const getGeminiThinkingBudgetRange = (model: string): { min: number; max: number } => {
    const normalized = (model || '').trim().toLowerCase()

    // Based on https://ai.google.dev/gemini-api/docs/thinking
    if (normalized.includes('flash-lite')) return { min: 512, max: 24576 }
    if (normalized.includes('flash')) return { min: 0, max: 24576 }
    if (normalized.includes('pro')) return { min: 128, max: 32768 }
    if (normalized.includes('robotics-er')) return { min: 0, max: 24576 }
    return { min: 128, max: 32768 }
}

export const clampGeminiThinkingBudget = (model: string, budget: number): number => {
    if (budget === -1) return -1
    const { min, max } = getGeminiThinkingBudgetRange(model)
    return Math.min(Math.max(budget, min), max)
}

export const clampGeminiImageNumberOfImages = (value: number): number => {
    if (!Number.isFinite(value)) return 1
    return Math.min(Math.max(Math.trunc(value), 1), 8)
}

export const maskApiKey = (key: string): string => {
    if (!key || key.length <= 8) return '••••••••'
    return key.substring(0, 4) + '••••••••' + key.substring(key.length - 4)
}
