import { useId } from 'react'
import { useT } from '../../contexts/I18nContext'
import { getPersonaCopy } from '../../i18n/personas'
import type { MemoryStats } from '../../services/memoryService'
import { Group, Row } from '../ui'

export const PERSONA_MEMORY_ROW_ID = 'persona-memory-row'

interface PersonaMemorySectionProps {
    /** `undefined` while loading, `null` when the summary is unavailable. */
    stats: MemoryStats | null | undefined
    onOpen: () => void
}

/** Summary row that opens the memory manager as a sub-page of the editor. */
const PersonaMemorySection: React.FC<PersonaMemorySectionProps> = ({ stats, onOpen }) => {
    const { t, locale } = useT()
    const copy = getPersonaCopy(locale).editor
    const summaryId = useId()
    const summary =
        stats === undefined ? copy.memoryLoading : stats ? copy.memorySummary(stats.total, stats.pinned) : null

    return (
        <Group title={copy.memory}>
            <Row
                id={PERSONA_MEMORY_ROW_ID}
                icon="memory"
                label={t('persona.memoryManagement')}
                aria-label={t('persona.memoryManagement')}
                aria-describedby={summary ? summaryId : undefined}
                detail={summary && <span id={summaryId}>{summary}</span>}
                onClick={onOpen}
            />
        </Group>
    )
}

export default PersonaMemorySection
