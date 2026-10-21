import { useState, useEffect, useRef } from 'react'
import { AnimatePresence, motion } from 'motion/react'
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
import { centerModalVariants, overlayVariants } from '../utils/motion'
import { getManagementCopy } from '../i18n/archive'
import { ModalOverlay } from './ModalOverlay'
import { Avatar, Icon } from './ui'
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
    const { t, locale } = useT()
    const copy = getManagementCopy(locale).conversation
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
            // Leave the dialog open; Escape only cancels the inline rename.
            e.preventDefault()
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
        <ModalOverlay
            className="prompt-modal-overlay chat-settings-overlay"
            aria-label={copy.more}
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={overlayVariants}
            onDismiss={handleClose}
        >
            <motion.div className="prompt-modal-card chat-settings-container" variants={centerModalVariants}>
                <div className="prompt-modal-header chat-settings-header">
                    <Avatar src={getAvatarSrc()} name={prompt.name} />
                    <div className="chat-settings-persona">
                        <h3>{prompt.name}</h3>
                        {prompt.description && <p>{prompt.description}</p>}
                    </div>
                    <button
                        type="button"
                        className="prompt-modal-close"
                        onClick={handleClose}
                        aria-label={t('common.close')}
                    >
                        <Icon name="x" />
                    </button>
                </div>

                <div className="prompt-modal-body chat-settings-body">
                    <section className="chat-settings-section">
                        <h4 className="chat-settings-section-title">{copy.title}</h4>
                        {editingTitle ? (
                            <div className="title-edit-container">
                                <input
                                    ref={titleInputRef}
                                    type="text"
                                    className="ui-input title-input"
                                    aria-label={t('chatSettings.chatTitle')}
                                    value={titleInput}
                                    onChange={(e) => setTitleInput(e.target.value)}
                                    onKeyDown={handleTitleKeyDown}
                                    placeholder={t('chatSettings.chatTitlePlaceholder')}
                                    disabled={savingTitle}
                                />
                                <div className="title-edit-actions">
                                    <button
                                        type="button"
                                        className="ui-btn ui-btn--sm ui-btn--ghost"
                                        onClick={handleCancelEdit}
                                        disabled={savingTitle}
                                    >
                                        {t('common.cancel')}
                                    </button>
                                    <button
                                        type="button"
                                        className="ui-btn ui-btn--sm ui-btn--primary"
                                        onClick={handleSaveTitle}
                                        disabled={!titleInput.trim() || savingTitle}
                                    >
                                        {savingTitle ? t('common.saving') : t('common.save')}
                                    </button>
                                </div>
                            </div>
                        ) : (
                            <button
                                type="button"
                                className="chat-settings-title-row"
                                onClick={() => setEditingTitle(true)}
                                aria-label={`${copy.rename}: ${currentSessionTitle || t('common.unnamed')}`}
                            >
                                <span className="chat-settings-title-text">
                                    {currentSessionTitle || t('common.unnamed')}
                                </span>
                                <Icon name="pencil" size={16} />
                            </button>
                        )}
                    </section>

                    <section className="chat-settings-section">
                        <h4 className="chat-settings-section-title">{copy.history}</h4>
                        {loading ? (
                            <div className="sessions-loading" role="status">
                                <span className="ui-spinner" aria-hidden="true" />
                                <span className="ui-visually-hidden">{t('common.loading')}</span>
                            </div>
                        ) : sessions.length === 0 ? (
                            <div className="sessions-empty">{t('chatSettings.noChatHistory')}</div>
                        ) : (
                            <ul className="sessions-list">
                                {sessions.map((session) => {
                                    const current = session.id === currentSessionId
                                    return (
                                        <li key={session.id} className={`session-item${current ? ' active' : ''}`}>
                                            <button
                                                type="button"
                                                className="session-open"
                                                onClick={() => handleSessionClick(session.id)}
                                                aria-current={current ? 'true' : undefined}
                                            >
                                                <span className="session-title">{session.title}</span>
                                                <span className="session-time">{formatTime(session.updated_at)}</span>
                                            </button>
                                            {current && <span className="session-current-badge">{copy.current}</span>}
                                            <button
                                                type="button"
                                                className="ui-icon-btn session-delete-btn"
                                                onClick={() => handleDeleteConfirm(session.id)}
                                                disabled={deletingSessionId === session.id}
                                                aria-label={`${t('common.delete')}: ${session.title}`}
                                                title={t('common.delete')}
                                            >
                                                <Icon name="trash" size={16} />
                                            </button>
                                        </li>
                                    )
                                })}
                            </ul>
                        )}
                    </section>
                </div>
            </motion.div>

            <AnimatePresence>
                {confirmDeleteId && (
                    <ModalOverlay
                        className="modal-overlay"
                        aria-label={t('chatSettings.deleteSession')}
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={overlayVariants}
                        onDismiss={handleCloseDeleteConfirm}
                    >
                        <motion.div className="modal-card" variants={centerModalVariants}>
                            <div className="modal-header">
                                <h3>{t('chatSettings.deleteSession')}</h3>
                            </div>
                            <div className="modal-body">
                                <p className="chat-settings-confirm-desc">
                                    {t('chatSettings.deleteSessionConfirm', { title: confirmSessionTitle })}
                                </p>
                            </div>
                            <div className="modal-footer">
                                <button
                                    type="button"
                                    className="modal-btn cancel"
                                    onClick={handleCloseDeleteConfirm}
                                    disabled={isDeletingConfirm}
                                >
                                    {t('common.cancel')}
                                </button>
                                <button
                                    type="button"
                                    className="modal-btn danger"
                                    onClick={handleDeleteSession}
                                    disabled={isDeletingConfirm}
                                >
                                    {isDeletingConfirm ? t('common.deleting') : t('common.delete')}
                                </button>
                            </div>
                        </motion.div>
                    </ModalOverlay>
                )}
            </AnimatePresence>
        </ModalOverlay>
    )
}

export default ChatSettings
