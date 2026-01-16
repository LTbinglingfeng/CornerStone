import type { PatParams, Prompt, ToolCall } from '../../../types/chat'

interface PatBannerProps {
    toolCall: ToolCall
    prompt: Prompt | null
}

export const PatBanner: React.FC<PatBannerProps> = ({ toolCall, prompt }) => {
    if (toolCall.function.name !== 'send_pat') return null

    try {
        const params: PatParams = JSON.parse(toolCall.function.arguments || '{}')
        const name = (typeof params.name === 'string' ? params.name.trim() : '') || prompt?.name || 'AI Assistant'
        const target = (typeof params.target === 'string' ? params.target.trim() : '') || '我'

        return (
            <div className="pat-banner">
                <span className="pat-banner-text">
                    "{name}"拍了拍{target}
                </span>
            </div>
        )
    } catch {
        return null
    }
}
