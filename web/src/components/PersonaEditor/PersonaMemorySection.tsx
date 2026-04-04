import { AnimatePresence, motion } from 'motion/react'
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
    return (
        <div className="persona-section persona-memory-section">
            <div className="persona-section-header" onClick={onToggle}>
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
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    {memoryCount > 0 && <span className="memory-count-badge">{memoryCount}</span>}
                    <motion.div
                        className="memory-chevron"
                        animate={{ rotate: expanded ? 90 : 0 }}
                        transition={{ duration: 0.2 }}
                    >
                        <svg viewBox="0 0 24 24">
                            <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6z" />
                        </svg>
                    </motion.div>
                </div>
            </div>
            <AnimatePresence initial={false}>
                {expanded && (
                    <motion.div
                        className="memory-content-wrapper"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: 'auto', opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }}
                        style={{ overflow: 'hidden' }}
                    >
                        <MemoryManager promptId={promptId} embedded onMemoryCountChange={onMemoryCountChange} />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    )
}

export default PersonaMemorySection
