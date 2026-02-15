import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Memory } from '../types/memory'
import { categoryLabels, selfCategories, userCategories } from '../types/memory'
import { memoryService, type MemoryExportItem, type MemoryStats } from '../services/memoryService'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import './MemoryManager.css'

interface MemoryManagerProps {
    promptId: string
}

const THRESHOLD_ACTIVE = 0.3
const THRESHOLD_ARCHIVE = 0.15

const MemoryManager: React.FC<MemoryManagerProps> = ({ promptId }) => {
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
        } catch (error) {
            console.error('Failed to load memories:', error)
        } finally {
            setLoading(false)
        }
    }, [promptId])

    useEffect(() => {
        loadMemories()
    }, [loadMemories])

    const handleDelete = async (memoryId: string) => {
        const ok = await confirm({
            title: '删除记忆',
            message: '确定删除这条记忆吗？',
            confirmText: '删除',
            danger: true,
        })
        if (!ok) return
        try {
            await memoryService.deleteMemory(promptId, memoryId)
            await loadMemories()
        } catch (error) {
            console.error('Failed to delete memory:', error)
            showToast('删除失败，请重试', 'error')
        }
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
            showToast(`已导出 ${data.length} 条记忆`, 'success')
        } catch (error) {
            console.error('Failed to export memories:', error)
            showToast('导出失败', 'error')
        }
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

    const activeMemories = filteredMemories.filter((m) => m.current_strength >= THRESHOLD_ACTIVE)
    const weakMemories = filteredMemories.filter(
        (m) => m.current_strength >= THRESHOLD_ARCHIVE && m.current_strength < THRESHOLD_ACTIVE
    )
    const archivedMemories = filteredMemories.filter((m) => m.current_strength < THRESHOLD_ARCHIVE)

    // 获取可用的 category 列表
    const availableCategories = useMemo(() => {
        if (filterSubject === 'user') return userCategories
        if (filterSubject === 'self') return selfCategories
        return [...userCategories, ...selfCategories]
    }, [filterSubject])

    // 当 subject 切换时重置 category
    useEffect(() => {
        if (filterCategory !== 'all' && !availableCategories.includes(filterCategory)) {
            setFilterCategory('all')
        }
    }, [filterSubject, filterCategory, availableCategories])

    if (!promptId) {
        return <div className="memory-manager loading">请先选择角色</div>
    }

    if (loading) {
        return <div className="memory-manager loading">加载中...</div>
    }

    return (
        <div className="memory-manager">
            <div className="memory-header">
                <h3>记忆管理</h3>
                <div className="memory-header-actions">
                    <button className="action-btn" onClick={() => setShowStats(!showStats)} title="统计">
                        {showStats ? '隐藏统计' : '统计'}
                    </button>
                    <button className="action-btn" onClick={handleExport} title="导出">
                        导出
                    </button>
                    <button className="action-btn" onClick={() => setShowImportModal(true)} title="导入">
                        导入
                    </button>
                    <button className="add-btn" onClick={() => setShowAddModal(true)}>
                        + 添加
                    </button>
                </div>
            </div>

            <div className="memory-hint">
                提示：记忆会保存在本地。开启长期记忆后，系统会将最近若干轮对话片段发送给记忆处理模型用于提取，请勿输入敏感信息。
            </div>

            {/* 统计面板 */}
            {showStats && stats && <StatsPanel stats={stats} />}

            {/* 搜索和筛选 */}
            <div className="memory-filters">
                <input
                    type="text"
                    className="memory-search"
                    placeholder="搜索记忆内容..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                />
                <select
                    className="memory-filter-select"
                    value={filterSubject}
                    onChange={(e) => setFilterSubject(e.target.value as 'all' | 'user' | 'self')}
                >
                    <option value="all">全部类型</option>
                    <option value="user">关于用户</option>
                    <option value="self">关于角色</option>
                </select>
                <select
                    className="memory-filter-select"
                    value={filterCategory}
                    onChange={(e) => setFilterCategory(e.target.value)}
                >
                    <option value="all">全部分类</option>
                    {availableCategories.map((c) => (
                        <option key={c} value={c}>
                            {categoryLabels[c]}
                        </option>
                    ))}
                </select>
            </div>

            {/* 筛选结果提示 */}
            {(searchQuery || filterSubject !== 'all' || filterCategory !== 'all') && (
                <div className="filter-result-hint">
                    共找到 {filteredMemories.length} 条记忆
                    {filteredMemories.length !== memories.length && ` (共 ${memories.length} 条)`}
                    <button className="clear-filter-btn" onClick={() => {
                        setSearchQuery('')
                        setFilterSubject('all')
                        setFilterCategory('all')
                    }}>
                        清除筛选
                    </button>
                </div>
            )}

            <section className="memory-section">
                <h4>活跃记忆 ({activeMemories.length})</h4>
                {activeMemories.length === 0 ? (
                    <p className="empty-hint">暂无活跃记忆</p>
                ) : (
                    activeMemories.map((m) => <MemoryItem key={m.id} memory={m} onDelete={() => handleDelete(m.id)} />)
                )}
            </section>

            {weakMemories.length > 0 && (
                <section className="memory-section">
                    <h4>待激活 ({weakMemories.length})</h4>
                    {weakMemories.map((m) => (
                        <MemoryItem key={m.id} memory={m} onDelete={() => handleDelete(m.id)} />
                    ))}
                </section>
            )}

            {archivedMemories.length > 0 && (
                <MemorySection
                    title={`已归档 (${archivedMemories.length})`}
                    memories={archivedMemories}
                    onDelete={handleDelete}
                    collapsible
                />
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
    const avgStrengthPercent = Math.round(stats.avg_strength * 100)

    return (
        <div className="memory-stats-panel">
            <div className="stats-row">
                <div className="stat-item">
                    <span className="stat-label">总计</span>
                    <span className="stat-value">{stats.total}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">活跃</span>
                    <span className="stat-value stat-active">{stats.active}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">待激活</span>
                    <span className="stat-value stat-weak">{stats.weak}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">归档</span>
                    <span className="stat-value stat-archived">{stats.archived}</span>
                </div>
            </div>
            <div className="stats-row">
                <div className="stat-item">
                    <span className="stat-label">平均强度</span>
                    <span className="stat-value">{avgStrengthPercent}%</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">总使用次数</span>
                    <span className="stat-value">{stats.total_seen_count}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">关于用户</span>
                    <span className="stat-value">{stats.by_subject['user'] || 0}</span>
                </div>
                <div className="stat-item">
                    <span className="stat-label">关于角色</span>
                    <span className="stat-value">{stats.by_subject['self'] || 0}</span>
                </div>
            </div>
            <div className="stats-categories">
                {Object.entries(stats.by_category).map(([cat, count]) => (
                    <span key={cat} className="stats-category-tag">
                        {categoryLabels[cat] || cat}: {count}
                    </span>
                ))}
            </div>
        </div>
    )
}

function MemoryItem({ memory, onDelete }: { memory: Memory; onDelete: () => void }) {
    const strengthPercent = Math.round((memory.current_strength || 0) * 100)

    return (
        <div className="memory-item">
            <div className="memory-content">
                <span className="memory-category">{categoryLabels[memory.category] || memory.category}</span>
                <span className="memory-text" title={memory.content}>
                    {memory.content}
                </span>
            </div>
            <div className="memory-meta">
                <div className="strength-bar">
                    <div className="strength-fill" style={{ width: `${strengthPercent}%` }} />
                </div>
                <span className="strength-value">{strengthPercent}%</span>
                <button className="delete-btn" onClick={onDelete} title="删除">
                    ×
                </button>
            </div>
        </div>
    )
}

function MemorySection({
    title,
    memories,
    onDelete,
    collapsible = false,
}: {
    title: string
    memories: Memory[]
    onDelete: (id: string) => void
    collapsible?: boolean
}) {
    const [collapsed, setCollapsed] = useState(collapsible)

    return (
        <section className="memory-section">
            <h4 className={collapsible ? 'collapsible' : ''} onClick={() => collapsible && setCollapsed(!collapsed)}>
                {title} {collapsible && (collapsed ? '▶' : '▼')}
            </h4>
            {!collapsed && memories.map((m) => <MemoryItem key={m.id} memory={m} onDelete={() => onDelete(m.id)} />)}
        </section>
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
    const { showToast } = useToast()
    const [subject, setSubject] = useState<'user' | 'self'>('user')
    const [category, setCategory] = useState('identity')
    const [content, setContent] = useState('')
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
            await memoryService.addMemory(promptId, { subject, category, content: content.trim() })
            onSuccess()
            onClose()
        } catch (error) {
            console.error('Failed to add memory:', error)
            showToast('添加失败，请重试', 'error')
        } finally {
            setSubmitting(false)
        }
    }

    return (
        <div className="memory-modal-overlay" onClick={onClose}>
            <div className="memory-modal" onClick={(e) => e.stopPropagation()}>
                <h3>添加记忆</h3>
                <form onSubmit={handleSubmit}>
                    <div className="memory-form-group">
                        <label>类型</label>
                        <select value={subject} onChange={(e) => setSubject(e.target.value as 'user' | 'self')}>
                            <option value="user">关于用户</option>
                            <option value="self">关于角色</option>
                        </select>
                    </div>

                    <div className="memory-form-group">
                        <label>分类</label>
                        <select value={category} onChange={(e) => setCategory(e.target.value)}>
                            {categories.map((c) => (
                                <option key={c} value={c}>
                                    {categoryLabels[c]}
                                </option>
                            ))}
                        </select>
                    </div>

                    <div className="memory-form-group">
                        <label>内容</label>
                        <input
                            type="text"
                            value={content}
                            onChange={(e) => setContent(e.target.value)}
                            placeholder={subject === 'user' ? '用户叫...' : '我答应...'}
                            required
                            autoFocus
                        />
                    </div>

                    <div className="memory-form-actions">
                        <button type="button" onClick={onClose} disabled={submitting}>
                            取消
                        </button>
                        <button type="submit" disabled={submitting || !content.trim()}>
                            {submitting ? '添加中...' : '添加'}
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
            showToast('文件格式无效，请选择正确的 JSON 文件', 'error')
            setPreviewData(null)
        }
    }

    const handleImport = async () => {
        if (!previewData || previewData.length === 0) return

        if (mode === 'replace') {
            const ok = await confirm({
                title: '替换模式',
                message: '替换模式会删除所有现有记忆，确定继续吗？',
                confirmText: '继续',
                danger: true,
            })
            if (!ok) return
        }

        setImporting(true)
        try {
            const result = await memoryService.importMemories(promptId, previewData, mode)
            showToast(`导入成功：${result.added} 条${result.invalid > 0 ? `，${result.invalid} 条无效` : ''}`, 'success')
            onSuccess()
            onClose()
        } catch (error) {
            console.error('Failed to import memories:', error)
            showToast('导入失败', 'error')
        } finally {
            setImporting(false)
        }
    }

    return (
        <div className="memory-modal-overlay" onClick={onClose}>
            <div className="memory-modal memory-modal-wide" onClick={(e) => e.stopPropagation()}>
                <h3>导入记忆</h3>

                <div className="memory-form-group">
                    <label>选择文件</label>
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
                            <p>将导入 {previewData.length} 条记忆</p>
                            <div className="import-preview-list">
                                {previewData.slice(0, 5).map((m, i) => (
                                    <div key={i} className="import-preview-item">
                                        <span className="memory-category">{categoryLabels[m.category] || m.category}</span>
                                        <span className="memory-text">{m.content}</span>
                                    </div>
                                ))}
                                {previewData.length > 5 && (
                                    <div className="import-preview-more">...还有 {previewData.length - 5} 条</div>
                                )}
                            </div>
                        </div>

                        <div className="memory-form-group">
                            <label>导入模式</label>
                            <select value={mode} onChange={(e) => setMode(e.target.value as 'merge' | 'replace')}>
                                <option value="merge">合并（保留现有记忆）</option>
                                <option value="replace">替换（删除现有记忆）</option>
                            </select>
                        </div>
                    </>
                )}

                <div className="memory-form-actions">
                    <button type="button" onClick={onClose} disabled={importing}>
                        取消
                    </button>
                    <button
                        type="button"
                        onClick={handleImport}
                        disabled={importing || !previewData || previewData.length === 0}
                    >
                        {importing ? '导入中...' : '导入'}
                    </button>
                </div>
            </div>
        </div>
    )
}

export default MemoryManager
