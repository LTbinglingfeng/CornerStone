export const DEFAULT_ASSISTANT_MESSAGE_SPLIT_TOKEN = '→'

export function resolveAssistantMessageSplitToken(token?: string | null): string {
    if (token === undefined || token === null) {
        return DEFAULT_ASSISTANT_MESSAGE_SPLIT_TOKEN
    }
    return token.trim()
}
