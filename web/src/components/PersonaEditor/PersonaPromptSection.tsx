import { useId } from 'react'
import { useT } from '../../contexts/I18nContext'
import { getPersonaCopy } from '../../i18n/personas'

interface PersonaPromptSectionProps {
    content: string
    onContentChange: (content: string) => void
}

const PersonaPromptSection: React.FC<PersonaPromptSectionProps> = ({ content, onContentChange }) => {
    const { locale } = useT()
    const copy = getPersonaCopy(locale).editor
    const promptId = useId()
    const hintId = useId()

    return (
        <section className="ui-group persona-prompt">
            <div className="ui-group__head">
                <h2 className="ui-group__title">
                    <label htmlFor={promptId}>{copy.prompt}</label>
                </h2>
                <span className="persona-prompt__count">{copy.characters(content.length)}</span>
            </div>
            <textarea
                id={promptId}
                required
                className="prompt-textarea"
                value={content}
                onChange={(e) => onContentChange(e.target.value)}
                placeholder={copy.promptPlaceholder}
                aria-describedby={hintId}
                rows={14}
            />
            <p id={hintId} className="ui-group__foot">
                {copy.promptHint}
            </p>
        </section>
    )
}

export default PersonaPromptSection
