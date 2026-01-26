import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import {
    getPrompts,
    createPrompt,
    updatePrompt,
    deletePrompt,
    uploadPromptAvatar,
    getPromptAvatarUrl,
    deletePromptAvatar,
    createSession,
    appendQueryParam,
} from '../services/api'
import type { Prompt } from '../types/chat'
import MemoryManager from './MemoryManager'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import './Contacts.css'

interface ContactsProps {
    onStartChat?: (sessionId: string, promptId: string) => void
}

const Contacts: React.FC<ContactsProps> = ({ onStartChat }) => {
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [loading, setLoading] = useState(true)
    const [showModal, setShowModal] = useState(false)
    const [editingPrompt, setEditingPrompt] = useState<Prompt | null>(null)
    const [activeTab, setActiveTab] = useState<'info' | 'memory'>('info')
    const [formData, setFormData] = useState({ name: '', content: '', description: '' })
    const [avatarFile, setAvatarFile] = useState<File | null>(null)
    const [avatarPreview, setAvatarPreview] = useState<string | null>(null)
    const [saving, setSaving] = useState(false)
    const modalRef = useRef<HTMLDivElement>(null)
    const fileInputRef = useRef<HTMLInputElement>(null)

    useEffect(() => {
        loadPrompts()
    }, [])

    useEffect(() => {
        if (showModal && modalRef.current) {
            gsap.fromTo(
                modalRef.current,
                { opacity: 0, scale: 0.9 },
                { opacity: 1, scale: 1, duration: 0.2, ease: 'power2.out' }
            )
        }
    }, [showModal])

    const loadPrompts = async () => {
        setLoading(true)
        const data = await getPrompts()
        setPrompts(data)
        setLoading(false)
    }

    const handleOpenModal = (prompt?: Prompt) => {
        setActiveTab('info')
        if (prompt) {
            setEditingPrompt(prompt)
            setFormData({
                name: prompt.name,
                content: prompt.content,
                description: prompt.description || '',
            })
            if (prompt.avatar) {
                setAvatarPreview(getPromptAvatarUrl(prompt.id))
            } else {
                setAvatarPreview(null)
            }
        } else {
            setEditingPrompt(null)
            setFormData({ name: '', content: '', description: '' })
            setAvatarPreview(null)
        }
        setAvatarFile(null)
        setShowModal(true)
    }

    const handleCloseModal = () => {
        if (modalRef.current) {
            gsap.to(modalRef.current, {
                opacity: 0,
                scale: 0.9,
                duration: 0.2,
                ease: 'power2.in',
                onComplete: () => {
                    setShowModal(false)
                    setEditingPrompt(null)
                    setActiveTab('info')
                    setFormData({ name: '', content: '', description: '' })
                    setAvatarFile(null)
                    setAvatarPreview(null)
                },
            })
        } else {
            setShowModal(false)
        }
    }

    const handleAvatarClick = () => {
        fileInputRef.current?.click()
    }

    const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0]
        if (file) {
            setAvatarFile(file)
            const reader = new FileReader()
            reader.onloadend = () => {
                setAvatarPreview(reader.result as string)
            }
            reader.readAsDataURL(file)
        }
    }

    const handleDeleteAvatar = async () => {
        if (editingPrompt && editingPrompt.avatar) {
            const success = await deletePromptAvatar(editingPrompt.id)
            if (success) {
                setAvatarPreview(null)
                setAvatarFile(null)
                showToast('头像已删除', 'success')
                loadPrompts()
            } else {
                showToast('删除头像失败', 'error')
            }
        } else {
            setAvatarPreview(null)
            setAvatarFile(null)
        }
    }

    const handleSave = async () => {
        if (!formData.name || !formData.content) {
            showToast('名称和内容不能为空', 'error')
            return
        }

        setSaving(true)
        try {
            if (editingPrompt) {
                // 更新
                const success = await updatePrompt(editingPrompt.id, formData)
                if (success) {
                    // 如果有新头像，上传
                    if (avatarFile) {
                        await uploadPromptAvatar(editingPrompt.id, avatarFile)
                    }
                    showToast('更新成功', 'success')
                    handleCloseModal()
                    loadPrompts()
                } else {
                    showToast('更新失败', 'error')
                }
            } else {
                // 创建
                const newPrompt = await createPrompt(formData)
                if (newPrompt) {
                    // 如果有头像，上传
                    if (avatarFile) {
                        await uploadPromptAvatar(newPrompt.id, avatarFile)
                    }
                    showToast('创建成功', 'success')
                    handleCloseModal()
                    loadPrompts()
                } else {
                    showToast('创建失败', 'error')
                }
            }
        } finally {
            setSaving(false)
        }
    }

    const handleDelete = async (prompt: Prompt) => {
        const ok = await confirm({
            title: '删除提示词',
            message: `确定要删除 "${prompt.name}" 吗？`,
            confirmText: '删除',
            danger: true,
        })
        if (ok) {
            const success = await deletePrompt(prompt.id)
            if (success) {
                showToast('删除成功', 'success')
                loadPrompts()
            } else {
                showToast('删除失败', 'error')
            }
        }
    }

    const handleStartChat = async (prompt: Prompt) => {
        if (!onStartChat) return
        const session = await createSession(prompt.name, prompt.id)
        if (session) {
            onStartChat(session.id, prompt.id)
        }
    }

    const getAvatarUrl = (prompt: Prompt) => {
        if (prompt.avatar) {
            return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
        }
        return null
    }

    return (
        <div className="contacts">
            <div className="contacts-header">
                <div style={{ width: 44 }}></div>
                <div className="contacts-title">通讯录</div>
                <button className="add-button" onClick={() => handleOpenModal()}>
                    <svg viewBox="0 0 24 24">
                        <path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z" />
                    </svg>
                </button>
            </div>

            <div className="contacts-content">
                {loading ? (
                    <div className="contacts-loading">加载中...</div>
                ) : prompts.length === 0 ? (
                    <div className="contacts-empty">
                        <div className="empty-icon">
                            <svg viewBox="0 0 24 24">
                                <path d="M19 3h-4.18C14.4 1.84 13.3 1 12 1c-1.3 0-2.4.84-2.82 2H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-7 0c.55 0 1 .45 1 1s-.45 1-1 1-1-.45-1-1 .45-1 1-1zm0 4c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm6 12H6v-1.4c0-2 4-3.1 6-3.1s6 1.1 6 3.1V19z" />
                            </svg>
                        </div>
                        <p>暂无提示词模板</p>
                        <p className="empty-hint">点击右上角 + 创建新的提示词</p>
                    </div>
                ) : (
                    <div className="contacts-list">
                        {prompts.map((prompt) => (
                            <div key={prompt.id} className="contact-item" onClick={() => handleOpenModal(prompt)}>
                                <div className="contact-avatar">
                                    {getAvatarUrl(prompt) ? (
                                        <img src={getAvatarUrl(prompt)!} alt={prompt.name} />
                                    ) : (
                                        <div className="avatar-placeholder">{prompt.name.charAt(0).toUpperCase()}</div>
                                    )}
                                </div>
                                <div className="contact-info">
                                    <div className="contact-name">{prompt.name}</div>
                                    <div className="contact-desc">
                                        {prompt.description ||
                                            prompt.content.substring(0, 50) + (prompt.content.length > 50 ? '...' : '')}
                                    </div>
                                </div>
                                <div className="contact-actions">
                                    {onStartChat && (
                                        <button
                                            className="contact-chat"
                                            onClick={(e) => {
                                                e.stopPropagation()
                                                handleStartChat(prompt)
                                            }}
                                        >
                                            <svg viewBox="0 0 24 24">
                                                <path d="M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z" />
                                            </svg>
                                        </button>
                                    )}
                                    <button
                                        className="contact-delete"
                                        onClick={(e) => {
                                            e.stopPropagation()
                                            handleDelete(prompt)
                                        }}
                                    >
                                        <svg viewBox="0 0 24 24">
                                            <path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z" />
                                        </svg>
                                    </button>
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {/* 编辑/创建弹窗 */}
            {showModal && (
                <div className="prompt-modal-overlay" onClick={handleCloseModal}>
                    <div className="prompt-modal-card" ref={modalRef} onClick={(e) => e.stopPropagation()}>
                        <div className="prompt-modal-header">
                            <h3>{editingPrompt ? '编辑提示词' : '新建提示词'}</h3>
                            <button className="prompt-modal-close" onClick={handleCloseModal}>
                                <svg viewBox="0 0 24 24">
                                    <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                                </svg>
                            </button>
                        </div>

                        <div className="prompt-modal-body">
                            <div className="prompt-modal-tabs">
                                <button
                                    type="button"
                                    className={`prompt-modal-tab ${activeTab === 'info' ? 'active' : ''}`}
                                    onClick={() => setActiveTab('info')}
                                >
                                    信息
                                </button>
                                <button
                                    type="button"
                                    className={`prompt-modal-tab ${activeTab === 'memory' ? 'active' : ''}`}
                                    onClick={() => setActiveTab('memory')}
                                    disabled={!editingPrompt}
                                >
                                    记忆
                                </button>
                            </div>

                            <div className={`prompt-tab-content ${activeTab === 'info' ? 'active' : ''}`}>
                                {/* 头像上传 */}
                                <div className="avatar-upload-container">
                                    <div className="avatar-upload" onClick={handleAvatarClick}>
                                        {avatarPreview ? (
                                            <img src={avatarPreview} alt="Avatar" className="avatar-preview" />
                                        ) : (
                                            <div className="avatar-placeholder-large">
                                                <svg viewBox="0 0 24 24">
                                                    <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" />
                                                </svg>
                                                <span>点击上传头像</span>
                                            </div>
                                        )}
                                        <input
                                            type="file"
                                            ref={fileInputRef}
                                            onChange={handleFileChange}
                                            accept="image/*"
                                            style={{ display: 'none' }}
                                        />
                                    </div>
                                    {avatarPreview && (
                                        <button className="avatar-delete-btn" onClick={handleDeleteAvatar}>
                                            删除头像
                                        </button>
                                    )}
                                </div>

                                {/* 名称输入 */}
                                <div className="form-group">
                                    <label>名称</label>
                                    <input
                                        type="text"
                                        value={formData.name}
                                        onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                                        placeholder="输入提示词名称"
                                    />
                                </div>

                                {/* 描述输入 */}
                                <div className="form-group">
                                    <label>描述（可选）</label>
                                    <input
                                        type="text"
                                        value={formData.description}
                                        onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                                        placeholder="简短描述"
                                    />
                                </div>

                                {/* 内容输入 */}
                                <div className="form-group" style={{ marginBottom: 0 }}>
                                    <label>内容</label>
                                    <textarea
                                        value={formData.content}
                                        onChange={(e) => setFormData({ ...formData, content: e.target.value })}
                                        placeholder="输入提示词内容..."
                                        rows={6}
                                    />
                                </div>
                            </div>

                            <div className={`prompt-tab-content ${activeTab === 'memory' ? 'active' : ''}`}>
                                {editingPrompt ? (
                                    <MemoryManager promptId={editingPrompt.id} />
                                ) : (
                                    <div
                                        style={{
                                            color: 'var(--text-secondary)',
                                            textAlign: 'center',
                                            padding: '20px 0',
                                        }}
                                    >
                                        请先保存角色后再管理记忆
                                    </div>
                                )}
                            </div>
                        </div>

                        <div className="prompt-modal-footer">
                            {activeTab === 'info' ? (
                                <>
                                    <button className="prompt-modal-btn cancel" onClick={handleCloseModal}>
                                        取消
                                    </button>
                                    <button className="prompt-modal-btn save" onClick={handleSave} disabled={saving}>
                                        {saving ? '保存中...' : '保存'}
                                    </button>
                                </>
                            ) : (
                                <button className="prompt-modal-btn save" onClick={handleCloseModal}>
                                    关闭
                                </button>
                            )}
                        </div>
                    </div>
                </div>
            )}
        </div>
    )
}

export default Contacts
