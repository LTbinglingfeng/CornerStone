interface PersonaPromptSectionProps {
    content: string
    onContentChange: (content: string) => void
}

const PersonaPromptSection: React.FC<PersonaPromptSectionProps> = ({ content, onContentChange }) => {
    const { t } = useT()
    const charCount = content.length

    return (
        <div className="persona-section">
            <div className="persona-section-header">
                <span className="section-title">
                    <svg className="section-icon" viewBox="0 0 24 24">
                        <path d="M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V8l-6-6zm-1 2l5 5h-5V4zM6 20V4h6v6h6v10H6zm2-6h8v2H8v-2zm0-3h8v2H8v-2z" />
                    </svg>
                    <span>{t('persona.personaPrompt')}</span>
                </span>
                <span className="section-badge">{charCount}</span>
            </div>
            <textarea
                className="prompt-textarea"
                value={content}
                onChange={(e) => onContentChange(e.target.value)}
                placeholder={t('persona.promptPlaceholder')}
            />
        </div>
    )
}

export default PersonaPromptSection
import { useT } from '../../contexts/I18nContext'
