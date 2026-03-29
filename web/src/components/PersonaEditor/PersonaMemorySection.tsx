import { AnimatePresence, motion } from 'motion/react'
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
    return (
        <div className="persona-section persona-memory-section">
            <div className="persona-section-header" onClick={onToggle}>
                <span className="section-title">
                    <svg className="section-icon" viewBox="0 0 24 24">
                        <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8zm-1-4h2v-2h-2v2zm1-12C9.24 4 7 6.24 7 9h2c0-1.66 1.34-3 3-3s3 1.34 3 3c0 3-4.5 2.62-4.5 7h2c0-3.15 4.5-3.5 4.5-7 0-2.76-2.24-5-5-5z" />
                    </svg>
                    <span>记忆管理</span>
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
                        <MemoryManager
                            promptId={promptId}
                            embedded
                            onMemoryCountChange={onMemoryCountChange}
                        />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    )
}

export default PersonaMemorySection
