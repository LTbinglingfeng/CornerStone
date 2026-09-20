import { useId } from 'react'
import { useT } from '../../contexts/I18nContext'
import MemoryManager from '../MemoryManager'

interface PersonaMemorySectionProps {
    promptId: string
    expanded: boolean
    onToggle: () => void
    memoryCount: number
    onMemoryCountChange: (count: number) => void
}

const PersonaMemorySection: React.FC<PersonaMemorySectionProps> = ({
    promptId,
    expanded,
    onToggle,
    memoryCount,
    onMemoryCountChange,
}) => {
    const { t } = useT()
    const contentId = useId()
    return (
        <div className="persona-section persona-memory-section">
            <button
                type="button"
                className="persona-section-header"
                onClick={onToggle}
                aria-expanded={expanded}
                aria-controls={contentId}
            >
                <span className="section-title">
                    <svg className="section-icon section-icon-memory" viewBox="0 0 24 24" aria-hidden="true">
                        <rect className="memory-icon-back" x="3.5" y="6" width="10.5" height="8.5" rx="2.5" />
                        <rect className="memory-icon-middle" x="6.5" y="4" width="11" height="8.75" rx="2.75" />
                        <rect className="memory-icon-front" x="8.5" y="8.25" width="12" height="9.25" rx="2.75" />
                        <path className="memory-icon-line" d="M11.5 11.75h6" strokeWidth="1.5" strokeLinecap="round" />
                        <path
                            className="memory-icon-line"
                            d="M11.5 14.75h4.25"
                            strokeWidth="1.5"
                            strokeLinecap="round"
                        />
                        <path
                            className="memory-icon-spark"
                            d="M18.75 3.3l.58 1.47 1.47.58-1.47.58-.58 1.47-.58-1.47-1.47-.58 1.47-.58z"
                        />
                    </svg>
                    <span>{t('persona.memoryManagement')}</span>
                </span>
                <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    {memoryCount > 0 && <span className="memory-count-badge">{memoryCount}</span>}
                    <span
                        className="memory-chevron"
                        aria-hidden="true"
                        style={{ transform: expanded ? 'rotate(90deg)' : undefined }}
                    >
                        <svg viewBox="0 0 24 24">
                            <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6z" />
                        </svg>
                    </span>
                </span>
            </button>
            <div id={contentId} className="memory-content-wrapper" hidden={!expanded}>
                {expanded && <MemoryManager promptId={promptId} embedded onMemoryCountChange={onMemoryCountChange} />}
            </div>
        </div>
    )
}

export default PersonaMemorySection
