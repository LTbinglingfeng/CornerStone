import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Memory } from '../types/memory'
import { getCategoryLabel, selfCategories, userCategories } from '../types/memory'
import { memoryService, type MemoryExportItem, type MemoryStats } from '../services/memoryService'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import CustomSelect from './provider/CustomSelect'
import type { SelectOption } from './provider/constants'
import './MemoryManager.css'

interface MemoryManagerProps {
    promptId: string
    embedded?: boolean
    onMemoryCountChange?: (count: number) => void
}

const THRESHOLD_ACTIVE = 0.3
const THRESHOLD_ARCHIVE = 0.15

const MemoryManager: React.FC<MemoryManagerProps> = ({ promptId, embedded, onMemoryCountChange }) => {
    const { t } = useT()
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

    if (!promptId) {
        return <div className={`${rootCls} loading`}>{t('memory.selectCharacterFirst')}</div>
    }

    if (loading) {
        return <div className={`${rootCls} loading`}>{t('common.loading')}</div>
    }

    return (
        <div className={rootCls}>
            <div className="memory-header">
                <h3>{t('memory.title')}</h3>
                <div className="memory-header-actions">
                    <button className="action-btn" onClick={() => setShowStats(!showStats)} title={t('memory.stats')}>
                        {showStats ? t('memory.hideStats') : t('memory.stats')}
                    </button>
                    <button className="action-btn" onClick={handleExport} title={t('common.export')}>
                        {t('common.export')}
                    </button>
                    <button className="action-btn" onClick={() => setShowImportModal(true)} title={t('common.import')}>
                        {t('common.import')}
                    </button>
                    {selectMode ? (
                        <button className="action-btn" onClick={exitSelectMode}>
                            {t('common.cancelSelect')}
                        </button>
                    ) : (
                        <button className="action-btn" onClick={() => setSelectMode(true)}>
                            {t('common.select')}
                        </button>
                    )}
                    <button className="add-btn" onClick={() => setShowAddModal(true)}>
                        + {t('common.add')}
                    </button>
                </div>
            </div>

            <div className="memory-hint">{t('memory.hint')}</div>

            {/* 统计面板 */}
            {showStats && stats && <StatsPanel stats={stats} />}

            {/* 搜索和筛选 */}
            <div className="memory-filters">
                <input
                    type="text"
                    className="memory-search"
                    placeholder={t('memory.searchPlaceholder')}
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                />
                <div className="memory-filter-select-wrapper">
                    <CustomSelect
                        value={filterSubject}
                        options={subjectOptions}
                        onChange={(v) => setFilterSubject(v as 'all' | 'user' | 'self')}
                        ariaLabel={t('memory.typeFilter')}
                    />
                </div>
                <div className="memory-filter-select-wrapper">
                    <CustomSelect
                        value={filterCategory}
                        options={categoryOptions}
                        onChange={setFilterCategory}
                        ariaLabel={t('memory.categoryFilter')}
                    />
                </div>
            </div>

            {/* 筛选结果提示 */}
            {(searchQuery || filterSubject !== 'all' || filterCategory !== 'all') && (
                <div className="filter-result-hint">
                    {t('memory.foundCount', { count: filteredMemories.length })}
                    {filteredMemories.length !== memories.length && t('memory.totalCount', { count: memories.length })}
                    <button
                        className="clear-filter-btn"
                        onClick={() => {
                            setSearchQuery('')
                            setFilterSubject('all')
                            setFilterCategory('all')
                        }}
                    >
                        {t('memory.clearFilter')}
                    </button>
                </div>
            )}

            {/* 永久记忆区域 */}
            {pinnedMemories.length > 0 && (
                <section className="memory-section">
                    <div className="memory-section-header">
                        <h4>
                            {t('memory.pinnedMemories')} ({pinnedMemories.length})
                        </h4>
                        {selectMode && (
                            <label className="memory-select-all">
                                <input
                                    type="checkbox"
                                    checked={pinnedMemories.every((m) => selectedIds.has(m.id))}
                                    onChange={() => toggleSelectAll(pinnedMemories)}
                                />
                                {t('common.selectAll')}
                            </label>
                        )}
                    </div>
                    {pinnedMemories.map((m) => (
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
                    ))}
                </section>
            )}

            {/* 活跃记忆 */}
            <section className="memory-section">
                <div className="memory-section-header">
                    <h4>
                        {t('memory.activeMemories')} ({activeMemories.length})
                    </h4>
                    {selectMode && activeMemories.length > 0 && (
                        <label className="memory-select-all">
                            <input
                                type="checkbox"
                                checked={activeMemories.every((m) => selectedIds.has(m.id))}
                                onChange={() => toggleSelectAll(activeMemories)}
                            />
                            {t('common.selectAll')}
                        </label>
                    )}
                </div>
                {activeMemories.length === 0 ? (
                    <p className="empty-hint">{t('memory.noActiveMemories')}</p>
                ) : (
                    activeMemories.map((m) => (
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
                )}
            </section>

            {/* 待激活 */}
            {weakMemories.length > 0 && (
                <section className="memory-section">
                    <div className="memory-section-header">
                        <h4>
                            {t('memory.weakMemories')} ({weakMemories.length})
                        </h4>
                        {selectMode && (
                            <label className="memory-select-all">
                                <input
                                    type="checkbox"
                                    checked={weakMemories.every((m) => selectedIds.has(m.id))}
                                    onChange={() => toggleSelectAll(weakMemories)}
                                />
                                {t('common.selectAll')}
                            </label>
                        )}
                    </div>
                    {weakMemories.map((m) => (
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
                    ))}
                </section>
            )}

            {/* 归档区域 */}
            {archivedMemories.length > 0 && (
                <section className="memory-section">
                    <div className="memory-section-header">
                        <h4 className="collapsible" onClick={() => setArchivedCollapsed(!archivedCollapsed)}>
                            {t('memory.archivedMemories')} ({archivedMemories.length}){' '}
                            {archivedCollapsed ? t('memory.expand') : t('memory.collapse')}
                        </h4>
                        {!archivedCollapsed && (
                            <>
                                <button className="clear-archived-btn" onClick={handleClearArchived}>
                                    {t('memory.clearArchived')}
                                </button>
                                {selectMode && (
                                    <label className="memory-select-all">
                                        <input
                                            type="checkbox"
                                            checked={archivedMemories.every((m) => selectedIds.has(m.id))}
                                            onChange={() => toggleSelectAll(archivedMemories)}
                                        />
                                        {t('common.selectAll')}
                                    </label>
                                )}
                            </>
                        )}
                    </div>
                    {!archivedCollapsed &&
                        archivedMemories.map((m) => (
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
                        ))}
                </section>
            )}

            {/* 批量操作浮动栏 */}
            {selectMode && selectedIds.size > 0 && (
                <div className="batch-action-bar">
                    <span>{t('memory.selectedCount', { count: selectedIds.size })}</span>
                    <button className="batch-delete-btn" onClick={handleBatchDelete}>
                        {t('memory.deleteSelected')}
                    </button>
                </div>
            )}

            {showAddModal && (
                <AddMemoryModal promptId={promptId} onClose={() => setShowAddModal(false)} onSuccess={loadMemories} />
            )}

            {showImportModal && (
                <ImportMemoryModal
                    promptId={promptId}
                    onClose={() => setShowImportModal(false)}
                    onSuccess={loadMemories}
                />
            )}
        </div>
    )
}

function StatsPanel({ stats }: { stats: MemoryStats }) {
    const { t } = useT()
    const avgStrengthPercent = Math.round(stats.avg_strength * 100)

    return (
        <div className="memory-stats-panel">
            <div className="stats-row">
                <div className="stat-item">
                    <span className="stat-label">{t('common.total')}</span>
                    <span className="stat-value">{stats.total}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">{t('memory.pinned')}</span>
                    <span className="stat-value stat-pinned">{stats.pinned}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">{t('memory.active')}</span>
                    <span className="stat-value stat-active">{stats.active}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">{t('memory.weak')}</span>
                    <span className="stat-value stat-weak">{stats.weak}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">{t('memory.archived')}</span>
                    <span className="stat-value stat-archived">{stats.archived}</span>
                </div>
            </div>
            <div className="stats-row">
                <div className="stat-item">
                    <span className="stat-label">{t('memory.avgStrength')}</span>
                    <span className="stat-value">{avgStrengthPercent}%</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">{t('memory.totalUsageCount')}</span>
                    <span className="stat-value">{stats.total_seen_count}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">{t('memory.aboutUser')}</span>
                    <span className="stat-value">{stats.by_subject['user'] || 0}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">{t('memory.aboutCharacter')}</span>
                    <span className="stat-value">{stats.by_subject['self'] || 0}</span>
                </div>
            </div>
            <div className="stats-categories">
                {Object.entries(stats.by_category).map(([cat, count]) => (
                    <span key={cat} className="stats-category-tag">
                        {getCategoryLabel(cat)}: {count}
                    </span>
                ))}
            </div>
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
    const { t } = useT()
    const strengthPercent = Math.round((memory.current_strength || 0) * 100)

    return (
        <div
            className={`memory-item${memory.pinned ? ' memory-item-pinned' : ''}${selected ? ' memory-item-selected' : ''}`}
        >
            {selectMode && (
                <input type="checkbox" className="memory-checkbox" checked={selected} onChange={onToggleSelect} />
            )}
            <div className="memory-content">
                <span className="memory-category">{getCategoryLabel(memory.category)}</span>
                <span className="memory-text" title={memory.content}>
                    {memory.content}
                </span>
            </div>
            <div className="memory-meta">
                {memory.pinned ? (
                    <span className="pinned-label">{t('memory.pinned')}</span>
                ) : editingStrength ? (
                    <div className="strength-edit">
                        <input
                            type="range"
                            className="strength-slider"
                            min="0"
                            max="100"
                            value={editingStrengthValue}
                            onChange={(e) => onStrengthChange(Number(e.target.value))}
                        />
                        <span className="strength-value">{editingStrengthValue}%</span>
                        <button
                            className="strength-edit-confirm"
                            onClick={onStrengthConfirm}
                            title={t('common.confirm')}
                        >
                            ✓
                        </button>
                        <button className="strength-edit-cancel" onClick={onStrengthCancel} title={t('common.cancel')}>
                            ✗
                        </button>
                    </div>
                ) : (
                    <>
                        <div className="strength-bar">
                            <div className="strength-fill" style={{ width: `${strengthPercent}%` }} />
                        </div>
                        <span
                            className="strength-value strength-value-editable"
                            onClick={onStrengthEdit}
                            title={t('memory.clickToModifyStrength')}
                        >
                            {strengthPercent}%
                        </span>
                    </>
                )}
                <button
                    className={`pin-btn${memory.pinned ? ' pin-btn-active' : ''}`}
                    onClick={onTogglePin}
                    title={memory.pinned ? t('memory.unpin') : t('memory.pinAsPermament')}
                >
                    📌
                </button>
                <button className="delete-btn" onClick={onDelete} title={t('common.delete')}>
                    ×
                </button>
            </div>
        </div>
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
    const { t } = useT()
    const { showToast } = useToast()
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
        <div className="memory-modal-overlay" onClick={onClose}>
            <div className="memory-modal" onClick={(e) => e.stopPropagation()}>
                <h3>{t('memory.addMemory')}</h3>
                <form onSubmit={handleSubmit}>
                    <div className="memory-form-group">
                        <label>{t('memory.memoryType')}</label>
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

                    <div className="memory-form-group">
                        <label>{t('memory.memoryCategory')}</label>
                        <CustomSelect
                            value={category}
                            options={categories.map((c) => ({ value: c, label: getCategoryLabel(c) }))}
                            onChange={setCategory}
                            ariaLabel={t('memory.memoryCategory')}
                        />
                    </div>

                    <div className="memory-form-group">
                        <label>{t('memory.content')}</label>
                        <input
                            type="text"
                            value={content}
                            onChange={(e) => setContent(e.target.value)}
                            placeholder={subject === 'user' ? t('memory.userPlaceholder') : t('memory.selfPlaceholder')}
                            required
                            autoFocus
                        />
                    </div>

                    <div className="memory-form-group memory-form-toggle">
                        <div className="modal-toggle-wrapper">
                            <label className="toggle-switch">
                                <input type="checkbox" checked={pinned} onChange={(e) => setPinned(e.target.checked)} />
                                <span className="toggle-slider"></span>
                            </label>
                            <span className="toggle-label">{t('memory.setPinned')}</span>
                        </div>
                    </div>

                    <div className="memory-form-actions">
                        <button type="button" onClick={onClose} disabled={submitting}>
                            {t('common.cancel')}
                        </button>
                        <button type="submit" disabled={submitting || !content.trim()}>
                            {submitting ? t('common.adding') : t('common.add')}
                        </button>
                    </div>
                </form>
            </div>
        </div>
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
    const { t } = useT()
    const { showToast } = useToast()
    const { confirm } = useConfirm()
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
        <div className="memory-modal-overlay" onClick={onClose}>
            <div className="memory-modal memory-modal-wide" onClick={(e) => e.stopPropagation()}>
                <h3>{t('memory.importMemory')}</h3>

                <div className="memory-form-group">
                    <label>{t('memory.selectFile')}</label>
                    <input
                        ref={fileInputRef}
                        type="file"
                        accept=".json"
                        onChange={handleFileSelect}
                        className="memory-file-input"
                    />
                </div>

                {previewData && (
                    <>
                        <div className="import-preview">
                            <p>{t('memory.importCount', { count: previewData.length })}</p>
                            <div className="import-preview-list">
                                {previewData.slice(0, 5).map((m, i) => (
                                    <div key={i} className="import-preview-item">
                                        <span className="memory-category">{getCategoryLabel(m.category)}</span>
                                        <span className="memory-text">{m.content}</span>
                                    </div>
                                ))}
                                {previewData.length > 5 && (
                                    <div className="import-preview-more">
                                        ...{t('memory.moreItems', { count: previewData.length - 5 })}
                                    </div>
                                )}
                            </div>
                        </div>

                        <div className="memory-form-group">
                            <label>{t('common.import')}</label>
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

                <div className="memory-form-actions">
                    <button type="button" onClick={onClose} disabled={importing}>
                        {t('common.cancel')}
                    </button>
                    <button
                        type="button"
                        onClick={handleImport}
                        disabled={importing || !previewData || previewData.length === 0}
                    >
                        {importing ? t('common.importing') : t('common.import')}
                    </button>
                </div>
            </div>
        </div>
    )
}

export default MemoryManager
