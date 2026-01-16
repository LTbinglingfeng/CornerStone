import { useCallback, useEffect, useState } from 'react'
import type { Memory } from '../types/memory'
import { categoryLabels, selfCategories, userCategories } from '../types/memory'
import { memoryService } from '../services/memoryService'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import './MemoryManager.css'

interface MemoryManagerProps {
    promptId: string
}

const MemoryManager: React.FC<MemoryManagerProps> = ({ promptId }) => {
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [memories, setMemories] = useState<Memory[]>([])
    const [showAddModal, setShowAddModal] = useState(false)
    const [loading, setLoading] = useState(true)

    const loadMemories = useCallback(async () => {
        if (!promptId) return
        setLoading(true)
        try {
            const data = await memoryService.getMemories(promptId)
            setMemories(data)
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

    const activeMemories = memories.filter((m) => m.current_strength >= 0.4)
    const weakMemories = memories.filter((m) => m.current_strength >= 0.15 && m.current_strength < 0.4)
    const archivedMemories = memories.filter((m) => m.current_strength < 0.15)

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
                <button className="add-btn" onClick={() => setShowAddModal(true)}>
                    + 添加
                </button>
            </div>

            <div className="memory-hint">
                提示：记忆会保存在本地。开启长期记忆后，系统会将最近若干轮对话片段发送给记忆处理模型用于提取，请勿输入敏感信息。
            </div>

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

export default MemoryManager
