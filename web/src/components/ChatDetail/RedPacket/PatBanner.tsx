import type { PatParams, Prompt, ToolCall } from '../../../types/chat'
import { useT } from '../../../contexts/I18nContext'

interface PatBannerProps {
    toolCall: ToolCall
    prompt: Prompt | null
}

export const PatBanner: React.FC<PatBannerProps> = ({ toolCall, prompt }) => {
    const { t } = useT()
    if (toolCall.function.name !== 'send_pat') return null

    try {
        const params: PatParams = JSON.parse(toolCall.function.arguments || '{}')
        const name =
            (typeof params.name === 'string' ? params.name.trim() : '') || prompt?.name || t('chat.defaultAIName')
        const target = (typeof params.target === 'string' ? params.target.trim() : '') || t('chat.defaultUserName')

        return (
            <div className="pat-banner">
                <span className="pat-banner-text">
                    "{name}" {t('chat.patAction')} {target}
                </span>
            </div>
        )
    } catch {
        return null
    }
}
