import { useState, useEffect, useRef } from 'react'
import { motion } from 'motion/react'
import type { ChatSession, Prompt } from '../types/chat'
import {
    getSessionsByPromptId,
    getPromptAvatarUrl,
    updateSessionTitle,
    appendQueryParam,
    deleteSession,
} from '../services/api'
import { useT } from '../contexts/I18nContext'
import { formatTime } from '../utils/time'
import { bottomSheetVariants, overlayVariants } from '../utils/motion'
import './ChatSettings.css'

interface ChatSettingsProps {
    prompt: Prompt
    currentSessionId: string
    currentSessionTitle?: string
    onClose: () => void
    onSwitchSession: (sessionId: string) => void
    onTitleUpdated?: (newTitle: string) => void
    onExitChat?: () => void
}

const ChatSettings: React.FC<ChatSettingsProps> = ({
    prompt,
    currentSessionId,
    currentSessionTitle,
    onClose,
    onSwitchSession,
    onTitleUpdated,
    onExitChat,
}) => {
    const { t } = useT()
    const [sessions, setSessions] = useState<ChatSession[]>([])
    const [loading, setLoading] = useState(true)
    const [editingTitle, setEditingTitle] = useState(false)
    const [titleInput, setTitleInput] = useState(currentSessionTitle || '')
    const [savingTitle, setSavingTitle] = useState(false)
    const [deletingSessionId, setDeletingSessionId] = useState<string | null>(null)
    const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null)
    const titleInputRef = useRef<HTMLInputElement>(null)

    useEffect(() => {
        loadSessions()
    }, [prompt.id])

    const loadSessions = async () => {
        setLoading(true)
        const data = await getSessionsByPromptId(prompt.id)
        // 按更新时间排序，最新的在前
        const sorted = data.sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
        setSessions(sorted)
        setLoading(false)
    }

    // 聚焦到输入框
    useEffect(() => {
        if (editingTitle && titleInputRef.current) {
            titleInputRef.current.focus()
            titleInputRef.current.select()
        }
    }, [editingTitle])

    const handleSaveTitle = async () => {
        if (!titleInput.trim() || savingTitle) return

        setSavingTitle(true)
        const success = await updateSessionTitle(currentSessionId, titleInput.trim())
        setSavingTitle(false)

        if (success) {
            setEditingTitle(false)
            // 更新本地 sessions 列表
            setSessions((prev) => prev.map((s) => (s.id === currentSessionId ? { ...s, title: titleInput.trim() } : s)))
            // 通知父组件
            if (onTitleUpdated) {
                onTitleUpdated(titleInput.trim())
            }
        }
    }

    const handleCancelEdit = () => {
        setTitleInput(currentSessionTitle || '')
        setEditingTitle(false)
    }

    const handleTitleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'Enter') {
            e.preventDefault()
            handleSaveTitle()
        } else if (e.key === 'Escape') {
            handleCancelEdit()
        }
    }

    const handleClose = () => {
        onClose()
    }

    const handleSessionClick = (sessionId: string) => {
        if (sessionId === currentSessionId) return
        handleClose()
        // 延迟切换以等待关闭动画完成
        setTimeout(() => {
            onSwitchSession(sessionId)
        }, 350)
    }

    const handleDeleteConfirm = (sessionId: string) => {
        setConfirmDeleteId(sessionId)
    }

    const handleCloseDeleteConfirm = () => {
        if (deletingSessionId) return
        setConfirmDeleteId(null)
    }

    const handleDeleteSession = async () => {
        if (!confirmDeleteId || deletingSessionId) return
        const sessionId = confirmDeleteId
        setDeletingSessionId(sessionId)
        const success = await deleteSession(sessionId)
        setDeletingSessionId(null)

        if (!success) return

        const nextSessions = sessions.filter((session) => session.id !== sessionId)
        setSessions(nextSessions)
        setConfirmDeleteId(null)

        if (sessionId === currentSessionId) {
            if (nextSessions.length > 0) {
                handleClose()
                setTimeout(() => {
                    onSwitchSession(nextSessions[0].id)
                }, 350)
            } else {
                handleClose()
                if (onExitChat) {
                    setTimeout(() => {
                        onExitChat()
                    }, 350)
                }
            }
        }
    }

    const getAvatarSrc = () => {
        if (prompt.avatar) {
            return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
        }
        return null
    }

    const confirmSessionTitle = confirmDeleteId
        ? sessions.find((session) => session.id === confirmDeleteId)?.title || t('common.unnamed')
        : ''
    const isDeletingConfirm = confirmDeleteId !== null && deletingSessionId === confirmDeleteId

    return (
        <motion.div
            className="chat-settings-overlay"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={overlayVariants}
            onClick={handleClose}
        >
            <motion.div
                className="chat-settings-container"
                initial="hidden"
                animate="visible"
                exit="hidden"
                variants={bottomSheetVariants}
                onClick={(e) => e.stopPropagation()}
            >
                {/* 顶部拖动条 */}
                <div className="chat-settings-handle">
                    <div className="handle-bar"></div>
                </div>

                {/* 角色信息 */}
                <div className="chat-settings-header">
                    <div className="prompt-avatar-large">
                        {getAvatarSrc() ? (
                            <img src={getAvatarSrc()!} alt={prompt.name} />
                        ) : (
                            <div className="avatar-placeholder">{prompt.name.charAt(0).toUpperCase()}</div>
                        )}
                    </div>
                    <div className="prompt-name">{prompt.name}</div>
                    {prompt.description && <div className="prompt-description">{prompt.description}</div>}
                </div>

                {/* 当前聊天设置 */}
                <div className="chat-settings-section">
                    <div className="section-title">{t('chatSettings.currentChat')}</div>
                    <div className="current-chat-card">
                        <div className="setting-row">
                            <span className="setting-label">{t('chatSettings.chatTitle')}</span>
                            {editingTitle ? (
                                <div className="title-edit-container">
                                    <input
                                        ref={titleInputRef}
                                        type="text"
                                        className="title-input"
                                        value={titleInput}
                                        onChange={(e) => setTitleInput(e.target.value)}
                                        onKeyDown={handleTitleKeyDown}
                                        placeholder={t('chatSettings.chatTitlePlaceholder')}
                                        disabled={savingTitle}
                                    />
                                    <div className="title-edit-actions">
                                        <button
                                            className="title-btn cancel"
                                            onClick={handleCancelEdit}
                                            disabled={savingTitle}
                                        >
                                            {t('common.cancel')}
                                        </button>
                                        <button
                                            className="title-btn save"
                                            onClick={handleSaveTitle}
                                            disabled={!titleInput.trim() || savingTitle}
                                        >
                                            {savingTitle ? t('common.saving') : t('common.save')}
                                        </button>
                                    </div>
                                </div>
                            ) : (
                                <div className="setting-value-row" onClick={() => setEditingTitle(true)}>
                                    <span className="setting-value">{currentSessionTitle || t('common.unnamed')}</span>
                                    <svg className="edit-icon" viewBox="0 0 24 24">
                                        <path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34c-.39-.39-1.02-.39-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z" />
                                    </svg>
                                </div>
                            )}
                        </div>
                    </div>
                </div>

                {/* 聊天记录列表 */}
                <div className="chat-settings-section">
                    <div className="section-title">{t('chatSettings.chatHistory')}</div>
                    <div className="sessions-card">
                        {loading ? (
                            <div className="sessions-loading">{t('common.loading')}</div>
                        ) : sessions.length === 0 ? (
                            <div className="sessions-empty">{t('chatSettings.noChatHistory')}</div>
                        ) : (
                            <div className="sessions-list">
                                {sessions.map((session) => (
                                    <div
                                        key={session.id}
                                        className={`session-item ${session.id === currentSessionId ? 'active' : ''}`}
                                        onClick={() => handleSessionClick(session.id)}
                                    >
                                        <div className="session-info">
                                            <div className="session-title">{session.title}</div>
                                            <div className="session-time">{formatTime(session.updated_at)}</div>
                                        </div>
                                        <div className="session-actions">
                                            {session.id === currentSessionId && (
                                                <div className="session-current-badge">{t('common.current')}</div>
                                            )}
                                            <button
                                                type="button"
                                                className="session-delete-btn"
                                                onClick={(event) => {
                                                    event.stopPropagation()
                                                    handleDeleteConfirm(session.id)
                                                }}
                                                disabled={deletingSessionId === session.id}
                                                title={t('chatSettings.deleteSession')}
                                            >
                                                <svg viewBox="0 0 24 24" aria-hidden="true">
                                                    <path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z" />
                                                </svg>
                                            </button>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                </div>
            </motion.div>

            {confirmDeleteId && (
                <div
                    className="chat-settings-confirm-overlay"
                    onClick={(event) => {
                        event.stopPropagation()
                        handleCloseDeleteConfirm()
                    }}
                >
                    <div className="chat-settings-confirm-card" onClick={(event) => event.stopPropagation()}>
                        <div className="chat-settings-confirm-title">{t('chatSettings.deleteSession')}</div>
                        <div className="chat-settings-confirm-desc">
                            {t('chatSettings.deleteSessionConfirm', { title: confirmSessionTitle })}
                        </div>
                        <div className="chat-settings-confirm-actions">
                            <button
                                type="button"
                                className="chat-settings-confirm-btn cancel"
                                onClick={handleCloseDeleteConfirm}
                                disabled={isDeletingConfirm}
                            >
                                {t('common.cancel')}
                            </button>
                            <button
                                type="button"
                                className="chat-settings-confirm-btn delete"
                                onClick={handleDeleteSession}
                                disabled={isDeletingConfirm}
                            >
                                {isDeletingConfirm ? t('common.deleting') : t('common.delete')}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </motion.div>
    )
}

export default ChatSettings
