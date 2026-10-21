import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import type { Memory } from '../types/memory'
import { getCategoryLabel, selfCategories, userCategories } from '../types/memory'
import { memoryService, type MemoryExportItem, type MemoryStats } from '../services/memoryService'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import CustomSelect from './provider/CustomSelect'
import type { SelectOption } from './provider/constants'
import { ModalOverlay } from './ModalOverlay'
import { AnimatePresence, motion } from 'motion/react'
import { centerModalVariants, overlayVariants } from '../utils/motion'
import { getPersonaCopy } from '../i18n/personas'
import { Badge, EmptyState, Icon } from './ui'
import './MemoryManager.css'
import './MemoryManagerModal.css'

interface MemoryManagerProps {
    promptId: string
    embedded?: boolean
    onMemoryCountChange?: (count: number) => void
}

const THRESHOLD_ACTIVE = 0.3
const THRESHOLD_ARCHIVE = 0.15

const MemoryManager: React.FC<MemoryManagerProps> = ({ promptId, embedded, onMemoryCountChange }) => {
    const { t, locale } = useT()
    const copy = getPersonaCopy(locale).memory
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [memories, setMemories] = useState<Memory[]>([])
    const [stats, setStats] = useState<MemoryStats | null>(null)
    const [showAddModal, setShowAddModal] = useState(false)
    const [showImportModal, setShowImportModal] = useState(false)
    const [showStats, setShowStats] = useState(false)
    const [loading, setLoading] = useState(true)

    // 搜索和筛选状态
    const [searchQuery, setSearchQuery] = useState('')
    const [filterSubject, setFilterSubject] = useState<'all' | 'user' | 'self'>('all')
    const [filterCategory, setFilterCategory] = useState<string>('all')

    // 选择模式状态
    const [selectMode, setSelectMode] = useState(false)
    const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

    // 强度编辑状态
    const [editingStrengthId, setEditingStrengthId] = useState<string | null>(null)
    const [editingStrengthValue, setEditingStrengthValue] = useState(0)

    // 归档区域折叠状态
    const [archivedCollapsed, setArchivedCollapsed] = useState(true)

    const loadMemories = useCallback(async () => {
        if (!promptId) return
        setLoading(true)
        try {
            const [data, statsData] = await Promise.all([
                memoryService.getMemories(promptId),
                memoryService.getMemoryStats(promptId),
            ])
            setMemories(data)
            setStats(statsData)
            onMemoryCountChange?.(data.length)
        } catch (error) {
            console.error('Failed to load memories:', error)
        } finally {
            setLoading(false)
        }
    }, [promptId, onMemoryCountChange])

    useEffect(() => {
        loadMemories()
    }, [loadMemories])

    const handleDelete = async (memoryId: string) => {
        const ok = await confirm({
            title: t('memory.deleteMemory'),
            message: t('memory.deleteMemoryConfirm'),
            confirmText: t('common.delete'),
            danger: true,
        })
        if (!ok) return
        try {
            await memoryService.deleteMemory(promptId, memoryId)
            await loadMemories()
        } catch (error) {
            console.error('Failed to delete memory:', error)
            showToast(t('memory.deleteFailed'), 'error')
        }
    }

    const handleTogglePin = async (memory: Memory) => {
        try {
            await memoryService.updateMemory(promptId, memory.id, { pinned: !memory.pinned } as Partial<Memory>)
            await loadMemories()
        } catch (error) {
            console.error('Failed to toggle pin:', error)
            showToast(t('common.operationFailed'), 'error')
        }
    }

    const handleStrengthEdit = (memory: Memory) => {
        setEditingStrengthId(memory.id)
        setEditingStrengthValue(Math.round(memory.current_strength * 100))
    }

    const handleStrengthConfirm = async (memoryId: string) => {
        try {
            await memoryService.updateMemory(promptId, memoryId, {
                strength: editingStrengthValue / 100,
            } as Partial<Memory>)
            setEditingStrengthId(null)
            await loadMemories()
        } catch (error) {
            console.error('Failed to update strength:', error)
            showToast(t('memory.modifyFailed'), 'error')
        }
    }

    const handleStrengthCancel = () => {
        setEditingStrengthId(null)
    }

    const handleExport = async () => {
        try {
            const data = await memoryService.exportMemories(promptId)
            const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
            const url = URL.createObjectURL(blob)
            const a = document.createElement('a')
            a.href = url
            a.download = `memories-${promptId}-${new Date().toISOString().slice(0, 10)}.json`
            a.click()
            URL.revokeObjectURL(url)
            showToast(t('memory.exported', { count: data.length }), 'success')
        } catch (error) {
            console.error('Failed to export memories:', error)
            showToast(t('memory.exportFailed'), 'error')
        }
    }

    // 批量删除
    const handleBatchDelete = async () => {
        if (selectedIds.size === 0) return
        const ok = await confirm({
            title: t('memory.batchDelete'),
            message: t('memory.batchDeleteConfirm', { count: selectedIds.size }),
            confirmText: t('common.delete'),
            danger: true,
        })
        if (!ok) return
        try {
            const result = await memoryService.batchDeleteMemories(promptId, Array.from(selectedIds))
            showToast(t('memory.batchDeleted', { count: result.deleted }), 'success')
            setSelectedIds(new Set())
            setSelectMode(false)
            await loadMemories()
        } catch (error) {
            console.error('Failed to batch delete:', error)
            showToast(t('memory.batchDeleteFailed'), 'error')
        }
    }

    // 清空归档
    const handleClearArchived = async () => {
        const ok = await confirm({
            title: t('memory.clearArchived'),
            message: t('memory.clearArchivedConfirm'),
            confirmText: t('memory.clear'),
            danger: true,
        })
        if (!ok) return
        try {
            const result = await memoryService.deleteArchivedMemories(promptId)
            showToast(t('memory.clearedArchived', { count: result.deleted }), 'success')
            await loadMemories()
        } catch (error) {
            console.error('Failed to clear archived:', error)
            showToast(t('memory.clearArchivedFailed'), 'error')
        }
    }

    // 选择模式逻辑
    const toggleSelect = (id: string) => {
        setSelectedIds((prev) => {
            const next = new Set(prev)
            if (next.has(id)) {
                next.delete(id)
            } else {
                next.add(id)
            }
            return next
        })
    }

    const toggleSelectAll = (memoryList: Memory[]) => {
        const ids = memoryList.map((m) => m.id)
        const allSelected = ids.every((id) => selectedIds.has(id))
        setSelectedIds((prev) => {
            const next = new Set(prev)
            if (allSelected) {
                ids.forEach((id) => next.delete(id))
            } else {
                ids.forEach((id) => next.add(id))
            }
            return next
        })
    }

    const exitSelectMode = () => {
        setSelectMode(false)
        setSelectedIds(new Set())
    }

    // 筛选后的记忆列表
    const filteredMemories = useMemo(() => {
        return memories.filter((m) => {
            // 关键词搜索
            if (searchQuery && !m.content.toLowerCase().includes(searchQuery.toLowerCase())) {
                return false
            }
            // subject 筛选
            if (filterSubject !== 'all' && m.subject !== filterSubject) {
                return false
            }
            // category 筛选
            if (filterCategory !== 'all' && m.category !== filterCategory) {
                return false
            }
            return true
        })
    }, [memories, searchQuery, filterSubject, filterCategory])

    // 分组：固定记忆、活跃、待激活、归档
    const pinnedMemories = filteredMemories.filter((m) => m.pinned)
    const activeMemories = filteredMemories.filter((m) => !m.pinned && m.current_strength >= THRESHOLD_ACTIVE)
    const weakMemories = filteredMemories.filter(
        (m) => !m.pinned && m.current_strength >= THRESHOLD_ARCHIVE && m.current_strength < THRESHOLD_ACTIVE
    )
    const archivedMemories = filteredMemories.filter((m) => !m.pinned && m.current_strength < THRESHOLD_ARCHIVE)

    // 获取可用的 category 列表
    const availableCategories = useMemo(() => {
        if (filterSubject === 'user') return userCategories
        if (filterSubject === 'self') return selfCategories
        return [...userCategories, ...selfCategories]
    }, [filterSubject])

    // CustomSelect 选项
    const subjectOptions: SelectOption[] = [
        { value: 'all', label: t('memory.allTypes') },
        { value: 'user', label: t('memory.aboutUser') },
        { value: 'self', label: t('memory.aboutCharacter') },
    ]

    const categoryOptions: SelectOption[] = useMemo(() => {
        return [
            { value: 'all', label: t('memory.allCategories') },
            ...availableCategories.map((c) => ({ value: c, label: getCategoryLabel(c) })),
        ]
    }, [availableCategories, t])

    // 当 subject 切换时重置 category
    useEffect(() => {
        if (filterCategory !== 'all' && !availableCategories.includes(filterCategory)) {
            setFilterCategory('all')
        }
    }, [filterSubject, filterCategory, availableCategories])

    const rootCls = `memory-manager${embedded ? ' embedded' : ''}`
    const hasFilters = Boolean(searchQuery || filterSubject !== 'all' || filterCategory !== 'all')
    const clearFilters = () => {
        setSearchQuery('')
        setFilterSubject('all')
        setFilterCategory('all')
    }

    if (!promptId) {
        return (
            <div className={rootCls}>
                <EmptyState icon="memory" title={t('memory.selectCharacterFirst')} />
            </div>
        )
    }

    // Only the first load blocks the view; later refreshes keep the list in place.
    if (loading && stats === null && memories.length === 0) {
        return (
            <div className={rootCls}>
                <EmptyState role="status" title={t('common.loading')} />
            </div>
        )
    }

    const renderItems = (list: Memory[]) =>
        list.map((m) => (
            <MemoryItem
                key={m.id}
                memory={m}
                onDelete={() => handleDelete(m.id)}
                onTogglePin={() => handleTogglePin(m)}
                selectMode={selectMode}
                selected={selectedIds.has(m.id)}
                onToggleSelect={() => toggleSelect(m.id)}
                editingStrength={editingStrengthId === m.id}
                editingStrengthValue={editingStrengthValue}
                onStrengthEdit={() => handleStrengthEdit(m)}
                onStrengthChange={setEditingStrengthValue}
                onStrengthConfirm={() => handleStrengthConfirm(m.id)}
                onStrengthCancel={handleStrengthCancel}
            />
        ))

    const renderSelectAll = (list: Memory[]) =>
        selectMode &&
        list.length > 0 && (
            <label className="memory-select-all">
                <input
                    type="checkbox"
                    checked={list.every((m) => selectedIds.has(m.id))}
                    onChange={() => toggleSelectAll(list)}
                />
                {t('common.selectAll')}
            </label>
        )

    const renderSection = (title: string, list: Memory[]) =>
        list.length > 0 && (
            <section className="memory-section">
                <div className="memory-section-header">
                    <h4 className="memory-section-title">
                        {title}
                        <span className="memory-section-count">{list.length}</span>
                    </h4>
                    {renderSelectAll(list)}
                </div>
                <div className="ui-list memory-list">{renderItems(list)}</div>
            </section>
        )

    return (
        <div className={rootCls}>
            <div className="memory-header">
                <p className="memory-summary">
                    {copy.summary(memories.length)}
                    {stats && stats.total > 0 && (
                        <span className="memory-summary__meta">
                            {copy.avgStrength} {Math.round(stats.avg_strength * 100)}%
                        </span>
                    )}
                </p>
                <div className="memory-header-actions">
                    <button
                        type="button"
                        className="ui-icon-btn"
                        onClick={() => setShowStats(!showStats)}
                        aria-pressed={showStats}
                        aria-label={showStats ? copy.hideStats : copy.stats}
                        title={showStats ? copy.hideStats : copy.stats}
                    >
                        <Icon name="chart" />
                    </button>
                    <button
                        type="button"
                        className="ui-icon-btn"
                        onClick={() => setShowImportModal(true)}
                        aria-label={copy.import}
                        title={copy.import}
                    >
                        <Icon name="upload" />
                    </button>
                    <button
                        type="button"
                        className="ui-icon-btn"
                        onClick={handleExport}
                        aria-label={copy.export}
                        title={copy.export}
                    >
                        <Icon name="download" />
                    </button>
                    {memories.length > 0 && (
                        <button
                            type="button"
                            className="ui-btn ui-btn--ghost ui-btn--sm"
                            aria-pressed={selectMode}
                            onClick={selectMode ? exitSelectMode : () => setSelectMode(true)}
                        >
                            {selectMode ? copy.done : copy.select}
                        </button>
                    )}
                    <button
                        type="button"
                        className="add-btn ui-btn ui-btn--primary ui-btn--sm"
                        onClick={() => setShowAddModal(true)}
                    >
                        <Icon name="plus" />
                        {copy.add}
                    </button>
                </div>
            </div>

            {showStats && stats && <StatsPanel stats={stats} />}

            {memories.length > 0 && (
                <div className="memory-filters">
                    <label className="memory-search">
                        <span className="ui-visually-hidden">{copy.searchLabel}</span>
                        <Icon name="search" size={16} className="memory-search__icon" />
                        <input
                            type="search"
                            className="ui-input"
                            placeholder={t('memory.searchPlaceholder')}
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                        />
                    </label>
                    <div className="memory-filter-select">
                        <CustomSelect
                            value={filterSubject}
                            options={subjectOptions}
                            onChange={(v) => setFilterSubject(v as 'all' | 'user' | 'self')}
                            ariaLabel={t('memory.typeFilter')}
                        />
                    </div>
                    <div className="memory-filter-select">
                        <CustomSelect
                            value={filterCategory}
                            options={categoryOptions}
                            onChange={setFilterCategory}
                            ariaLabel={t('memory.categoryFilter')}
                        />
                    </div>
                </div>
            )}

            {hasFilters && filteredMemories.length > 0 && (
                <div className="filter-result-hint" aria-live="polite">
                    <span>
                        {t('memory.foundCount', { count: filteredMemories.length })}
                        {filteredMemories.length !== memories.length &&
                            t('memory.totalCount', { count: memories.length })}
                    </span>
                    <button type="button" className="ui-btn ui-btn--ghost ui-btn--sm" onClick={clearFilters}>
                        {t('memory.clearFilter')}
                    </button>
                </div>
            )}

            {memories.length === 0 ? (
                <EmptyState icon="memory" title={copy.empty} description={copy.emptyHint} />
            ) : filteredMemories.length === 0 ? (
                <EmptyState
                    icon="search"
                    title={copy.noResults}
                    action={
                        <button type="button" className="ui-btn" onClick={clearFilters}>
                            {t('memory.clearFilter')}
                        </button>
                    }
                />
            ) : (
                <>
                    {renderSection(t('memory.pinnedMemories'), pinnedMemories)}
                    {renderSection(t('memory.activeMemories'), activeMemories)}
                    {renderSection(t('memory.weakMemories'), weakMemories)}

                    {archivedMemories.length > 0 && (
                        <section className="memory-section">
                            <div className="memory-section-header">
                                <button
                                    type="button"
                                    className="memory-section-toggle"
                                    aria-expanded={!archivedCollapsed}
                                    onClick={() => setArchivedCollapsed(!archivedCollapsed)}
                                >
                                    <Icon
                                        name="chevronRight"
                                        size={16}
                                        className={`memory-section-chevron${archivedCollapsed ? '' : ' is-open'}`}
                                    />
                                    <span className="memory-section-title">
                                        {t('memory.archivedMemories')}
                                        <span className="memory-section-count">{archivedMemories.length}</span>
                                    </span>
                                </button>
                                {!archivedCollapsed && (
                                    <div className="memory-section-actions">
                                        {renderSelectAll(archivedMemories)}
                                        <button
                                            type="button"
                                            className="clear-archived-btn ui-btn ui-btn--ghost ui-btn--sm"
                                            onClick={handleClearArchived}
                                        >
                                            {t('memory.clearArchived')}
                                        </button>
                                    </div>
                                )}
                            </div>
                            {!archivedCollapsed && (
                                <div className="ui-list memory-list">{renderItems(archivedMemories)}</div>
                            )}
                        </section>
                    )}
                </>
            )}

            <p className="memory-hint">{t('memory.hint')}</p>

            {selectMode && selectedIds.size > 0 && (
                <div className="batch-action-bar" role="region" aria-label={t('memory.batchDelete')}>
                    <span>{t('memory.selectedCount', { count: selectedIds.size })}</span>
                    <button
                        type="button"
                        className="batch-delete-btn ui-btn ui-btn--danger-solid ui-btn--sm"
                        onClick={handleBatchDelete}
                    >
                        <Icon name="trash" />
                        {t('memory.deleteSelected')}
                    </button>
                </div>
            )}

            <AnimatePresence>
                {showAddModal && (
                    <AddMemoryModal
                        key="add-memory"
                        promptId={promptId}
                        onClose={() => setShowAddModal(false)}
                        onSuccess={loadMemories}
                    />
                )}
                {showImportModal && (
                    <ImportMemoryModal
                        key="import-memory"
                        promptId={promptId}
                        onClose={() => setShowImportModal(false)}
                        onSuccess={loadMemories}
                    />
                )}
            </AnimatePresence>
        </div>
    )
}

function StatsPanel({ stats }: { stats: MemoryStats }) {
    const { t, locale } = useT()
    const copy = getPersonaCopy(locale).memory
    const cells: Array<[string, string | number]> = [
        [t('common.total'), stats.total],
        [t('memory.pinned'), stats.pinned],
        [t('memory.active'), stats.active],
        [t('memory.weak'), stats.weak],
        [t('memory.archived'), stats.archived],
        [copy.avgStrength, `${Math.round(stats.avg_strength * 100)}%`],
        [t('memory.totalUsageCount'), stats.total_seen_count],
        [t('memory.aboutUser'), stats.by_subject['user'] || 0],
        [t('memory.aboutCharacter'), stats.by_subject['self'] || 0],
    ]
    const categories = Object.entries(stats.by_category)

    return (
        <div className="memory-stats-panel">
            <dl className="memory-stats-grid">
                {cells.map(([label, value]) => (
                    <div key={label} className="memory-stat">
                        <dt>{label}</dt>
                        <dd>{value}</dd>
                    </div>
                ))}
            </dl>
            {categories.length > 0 && (
                <div className="memory-stats-categories">
                    {categories.map(([cat, count]) => (
                        <Badge key={cat}>
                            {getCategoryLabel(cat)} {count}
                        </Badge>
                    ))}
                </div>
            )}
        </div>
    )
}

function MemoryItem({
    memory,
    onDelete,
    onTogglePin,
    selectMode,
    selected,
    onToggleSelect,
    editingStrength,
    editingStrengthValue,
    onStrengthEdit,
    onStrengthChange,
    onStrengthConfirm,
    onStrengthCancel,
}: {
    memory: Memory
    onDelete: () => void
    onTogglePin: () => void
    selectMode: boolean
    selected: boolean
    onToggleSelect: () => void
    editingStrength: boolean
    editingStrengthValue: number
    onStrengthEdit: () => void
    onStrengthChange: (value: number) => void
    onStrengthConfirm: () => void
    onStrengthCancel: () => void
}) {
    const { t, locale } = useT()
    const copy = getPersonaCopy(locale).memory
    const strengthPercent = Math.round((memory.current_strength || 0) * 100)
    const created = new Date(memory.created_at)
    const createdLabel = Number.isNaN(created.getTime())
        ? ''
        : created.toLocaleDateString(locale === 'zh' ? 'zh-CN' : 'en', { month: 'short', day: 'numeric' })

    return (
        <div
            className={`memory-item${memory.pinned ? ' memory-item-pinned' : ''}${selected ? ' memory-item-selected' : ''}`}
        >
            {selectMode && (
                <input
                    type="checkbox"
                    className="memory-checkbox"
                    checked={selected}
                    onChange={onToggleSelect}
                    aria-label={`${copy.selectMemory}: ${memory.content}`}
                />
            )}
            <div className="memory-item__main">
                <p className="memory-text">{memory.content}</p>
                <div className="memory-meta">
                    <Badge>{getCategoryLabel(memory.category)}</Badge>
                    <span>{memory.subject === 'self' ? t('memory.aboutCharacter') : t('memory.aboutUser')}</span>
                    {memory.pinned ? (
                        <span className="memory-meta__pinned">
                            <Icon name="pin" size={12} />
                            {t('memory.pinned')}
                        </span>
                    ) : editingStrength ? (
                        <span className="strength-edit">
                            <input
                                type="range"
                                className="strength-slider"
                                min="0"
                                max="100"
                                value={editingStrengthValue}
                                aria-label={copy.strength}
                                onChange={(e) => onStrengthChange(Number(e.target.value))}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') onStrengthConfirm()
                                    if (e.key === 'Escape') {
                                        e.stopPropagation()
                                        onStrengthCancel()
                                    }
                                }}
                                autoFocus
                            />
                            <span className="strength-value">{editingStrengthValue}%</span>
                            <button
                                type="button"
                                className="ui-icon-btn strength-edit-btn"
                                onClick={onStrengthConfirm}
                                aria-label={t('common.confirm')}
                                title={t('common.confirm')}
                            >
                                <Icon name="check" />
                            </button>
                            <button
                                type="button"
                                className="ui-icon-btn strength-edit-btn"
                                onClick={onStrengthCancel}
                                aria-label={t('common.cancel')}
                                title={t('common.cancel')}
                            >
                                <Icon name="x" />
                            </button>
                        </span>
                    ) : (
                        <button
                            type="button"
                            className="memory-strength"
                            onClick={onStrengthEdit}
                            aria-label={copy.editStrength(strengthPercent)}
                            title={t('memory.clickToModifyStrength')}
                        >
                            <span className="strength-bar" aria-hidden="true">
                                <span className="strength-fill" style={{ width: `${strengthPercent}%` }} />
                            </span>
                            {strengthPercent}%
                        </button>
                    )}
                    {memory.seen_count > 0 && <span>{copy.seen(memory.seen_count)}</span>}
                    {createdLabel && <time dateTime={memory.created_at}>{createdLabel}</time>}
                </div>
            </div>
            <div className="memory-item__actions">
                <button
                    type="button"
                    className={`ui-icon-btn pin-btn${memory.pinned ? ' pin-btn-active' : ''}`}
                    onClick={onTogglePin}
                    aria-pressed={memory.pinned}
                    aria-label={memory.pinned ? copy.unpin : copy.pin}
                    title={memory.pinned ? t('memory.unpin') : t('memory.pinAsPermament')}
                >
                    <Icon name="pin" size={16} />
                </button>
                <button
                    type="button"
                    className="ui-icon-btn delete-btn"
                    onClick={onDelete}
                    aria-label={copy.delete}
                    title={copy.delete}
                >
                    <Icon name="trash" size={16} />
                </button>
            </div>
        </div>
    )
}

function MemoryModal({
    label,
    busy,
    onClose,
    children,
}: {
    label: string
    busy: boolean
    onClose: () => void
    children: React.ReactNode
}) {
    const { t } = useT()
    return (
        <ModalOverlay
            className="prompt-modal-overlay memory-modal-overlay"
            aria-label={label}
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={overlayVariants}
            onDismiss={() => {
                if (!busy) onClose()
            }}
        >
            <motion.div
                className="prompt-modal-card memory-modal"
                initial="hidden"
                animate="visible"
                exit="hidden"
                variants={centerModalVariants}
            >
                <div className="prompt-modal-header">
                    <h3>{label}</h3>
                    <button
                        type="button"
                        className="prompt-modal-close"
                        aria-label={t('common.close')}
                        onClick={onClose}
                        disabled={busy}
                    >
                        <Icon name="x" />
                    </button>
                </div>
                {children}
            </motion.div>
        </ModalOverlay>
    )
}

function AddMemoryModal({
    promptId,
    onClose,
    onSuccess,
}: {
    promptId: string
    onClose: () => void
    onSuccess: () => void
}) {
    const { t, locale } = useT()
    const copy = getPersonaCopy(locale).memory
    const { showToast } = useToast()
    const contentId = useId()
    const contentHintId = useId()
    const pinnedId = useId()
    const pinnedHintId = useId()
    const [subject, setSubject] = useState<'user' | 'self'>('user')
    const [category, setCategory] = useState('identity')
    const [content, setContent] = useState('')
    const [pinned, setPinned] = useState(false)
    const [submitting, setSubmitting] = useState(false)

    const categories = subject === 'user' ? userCategories : selfCategories

    useEffect(() => {
        setCategory(categories[0])
    }, [subject, categories])

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault()
        if (!content.trim()) return

        setSubmitting(true)
        try {
            await memoryService.addMemory(promptId, {
                subject,
                category,
                content: content.trim(),
                pinned,
            })
            onSuccess()
            onClose()
        } catch (error) {
            console.error('Failed to add memory:', error)
            showToast(t('memory.addFailed'), 'error')
        } finally {
            setSubmitting(false)
        }
    }

    return (
        <MemoryModal label={t('memory.addMemory')} busy={submitting} onClose={onClose}>
            <form className="memory-modal-form" onSubmit={handleSubmit}>
                <div className="prompt-modal-body">
                    <div className="ui-field">
                        <label className="ui-field__label" htmlFor={contentId}>
                            {t('memory.content')}
                        </label>
                        <textarea
                            id={contentId}
                            className="ui-textarea memory-content-input"
                            value={content}
                            onChange={(e) => setContent(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                                    e.preventDefault()
                                    e.currentTarget.form?.requestSubmit()
                                }
                            }}
                            placeholder={subject === 'user' ? t('memory.userPlaceholder') : t('memory.selfPlaceholder')}
                            aria-describedby={contentHintId}
                            rows={3}
                            required
                            autoFocus
                        />
                        <span id={contentHintId} className="ui-field__hint">
                            {copy.contentHint}
                        </span>
                    </div>

                    <div className="memory-form-row">
                        <div className="ui-field">
                            <span className="ui-field__label" aria-hidden="true">
                                {t('memory.memoryType')}
                            </span>
                            <CustomSelect
                                value={subject}
                                options={[
                                    { value: 'user', label: t('memory.aboutUser') },
                                    { value: 'self', label: t('memory.aboutCharacter') },
                                ]}
                                onChange={(v) => setSubject(v as 'user' | 'self')}
                                ariaLabel={t('memory.memoryType')}
                            />
                        </div>
                        <div className="ui-field">
                            <span className="ui-field__label" aria-hidden="true">
                                {t('memory.memoryCategory')}
                            </span>
                            <CustomSelect
                                value={category}
                                options={categories.map((c) => ({ value: c, label: getCategoryLabel(c) }))}
                                onChange={setCategory}
                                ariaLabel={t('memory.memoryCategory')}
                            />
                        </div>
                    </div>

                    <div className="memory-form-toggle">
                        <div className="memory-form-toggle__text">
                            <label htmlFor={pinnedId} className="ui-field__label">
                                {t('memory.setPinned')}
                            </label>
                            <span id={pinnedHintId} className="ui-field__hint">
                                {copy.pinnedHint}
                            </span>
                        </div>
                        <label className="toggle-switch">
                            <input
                                id={pinnedId}
                                type="checkbox"
                                role="switch"
                                aria-describedby={pinnedHintId}
                                checked={pinned}
                                onChange={(e) => setPinned(e.target.checked)}
                            />
                            <span className="toggle-slider" aria-hidden="true"></span>
                        </label>
                    </div>
                </div>

                <div className="prompt-modal-footer">
                    <button type="button" className="prompt-modal-btn cancel" onClick={onClose} disabled={submitting}>
                        {t('common.cancel')}
                    </button>
                    <button type="submit" className="prompt-modal-btn save" disabled={submitting || !content.trim()}>
                        {submitting ? t('common.adding') : t('common.add')}
                    </button>
                </div>
            </form>
        </MemoryModal>
    )
}

function ImportMemoryModal({
    promptId,
    onClose,
    onSuccess,
}: {
    promptId: string
    onClose: () => void
    onSuccess: () => void
}) {
    const { t, locale } = useT()
    const copy = getPersonaCopy(locale).memory
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const fileId = useId()
    const fileHintId = useId()
    const [mode, setMode] = useState<'merge' | 'replace'>('merge')
    const [importing, setImporting] = useState(false)
    const [previewData, setPreviewData] = useState<MemoryExportItem[] | null>(null)
    const fileInputRef = useRef<HTMLInputElement>(null)

    const handleFileSelect = async (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0]
        if (!file) return

        try {
            const text = await file.text()
            const data = JSON.parse(text) as MemoryExportItem[]
            if (!Array.isArray(data)) {
                throw new Error('Invalid format')
            }
            setPreviewData(data)
        } catch {
            showToast(t('memory.invalidFileFormat'), 'error')
            setPreviewData(null)
        }
    }

    const handleImport = async () => {
        if (!previewData || previewData.length === 0) return

        if (mode === 'replace') {
            const ok = await confirm({
                title: t('memory.replaceModeTitle'),
                message: t('memory.replaceModeConfirm'),
                confirmText: t('common.continue'),
                danger: true,
            })
            if (!ok) return
        }

        setImporting(true)
        try {
            const result = await memoryService.importMemories(promptId, previewData, mode)
            const invalidText = result.invalid > 0 ? t('memory.invalidSuffix', { count: result.invalid }) : ''
            showToast(t('memory.importSuccess', { added: result.added, invalid: invalidText }), 'success')
            onSuccess()
            onClose()
        } catch (error) {
            console.error('Failed to import memories:', error)
            showToast(t('memory.importFailed'), 'error')
        } finally {
            setImporting(false)
        }
    }

    return (
        <MemoryModal label={t('memory.importMemory')} busy={importing} onClose={onClose}>
            <div className="prompt-modal-body">
                <div className="ui-field">
                    <label className="ui-field__label" htmlFor={fileId}>
                        {t('memory.selectFile')}
                    </label>
                    <input
                        id={fileId}
                        ref={fileInputRef}
                        type="file"
                        accept=".json"
                        onChange={handleFileSelect}
                        className="memory-file-input"
                        aria-describedby={fileHintId}
                    />
                    <span id={fileHintId} className="ui-field__hint">
                        {copy.fileHint}
                    </span>
                </div>

                {previewData && (
                    <>
                        <div className="import-preview">
                            <p className="import-preview-title">{copy.preview(previewData.length)}</p>
                            <div className="ui-list import-preview-list">
                                {previewData.slice(0, 5).map((m, i) => (
                                    <div key={i} className="import-preview-item">
                                        <Badge>{getCategoryLabel(m.category)}</Badge>
                                        <span className="import-preview-text">{m.content}</span>
                                    </div>
                                ))}
                                {previewData.length > 5 && (
                                    <div className="import-preview-more">
                                        {t('memory.moreItems', { count: previewData.length - 5 })}
                                    </div>
                                )}
                            </div>
                        </div>

                        <div className="ui-field">
                            <span className="ui-field__label" aria-hidden="true">
                                {copy.mode}
                            </span>
                            <CustomSelect
                                value={mode}
                                options={[
                                    { value: 'merge', label: t('memory.mergeMode') },
                                    { value: 'replace', label: t('memory.replaceMode') },
                                ]}
                                onChange={(v) => setMode(v as 'merge' | 'replace')}
                                ariaLabel={t('common.import')}
                            />
                        </div>
                    </>
                )}
            </div>

            <div className="prompt-modal-footer">
                <button type="button" className="prompt-modal-btn cancel" onClick={onClose} disabled={importing}>
                    {t('common.cancel')}
                </button>
                <button
                    type="button"
                    className="prompt-modal-btn save"
                    onClick={handleImport}
                    disabled={importing || !previewData || previewData.length === 0}
                >
                    {importing ? t('common.importing') : t('common.import')}
                </button>
            </div>
        </MemoryModal>
    )
}

export default MemoryManager
