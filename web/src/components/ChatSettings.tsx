import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import type { ChatSession, Prompt } from '../types/chat'
import { getSessionsByPromptId, getPromptAvatarUrl, updateSessionTitle, appendQueryParam } from '../services/api'
import { formatTime } from '../utils/time'
import './ChatSettings.css'

interface ChatSettingsProps {
  prompt: Prompt
  currentSessionId: string
  currentSessionTitle?: string
  onClose: () => void
  onSwitchSession: (sessionId: string) => void
  onTitleUpdated?: (newTitle: string) => void
}

const ChatSettings: React.FC<ChatSettingsProps> = ({
  prompt,
  currentSessionId,
  currentSessionTitle,
  onClose,
  onSwitchSession,
  onTitleUpdated
}) => {
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [loading, setLoading] = useState(true)
  const [editingTitle, setEditingTitle] = useState(false)
  const [titleInput, setTitleInput] = useState(currentSessionTitle || '')
  const [savingTitle, setSavingTitle] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const overlayRef = useRef<HTMLDivElement>(null)
  const titleInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    // 入场动画
    if (overlayRef.current && containerRef.current) {
      gsap.fromTo(overlayRef.current, { opacity: 0 }, { opacity: 1, duration: 0.2 })
      gsap.fromTo(
        containerRef.current,
        { y: '100%' },
        { y: '0%', duration: 0.3, ease: 'power2.out' }
      )
    }
    loadSessions()
  }, [prompt.id])

  const loadSessions = async () => {
    setLoading(true)
    const data = await getSessionsByPromptId(prompt.id)
    // 按更新时间排序，最新的在前
    const sorted = data.sort((a, b) =>
      new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    )
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
      setSessions(prev => prev.map(s =>
        s.id === currentSessionId ? { ...s, title: titleInput.trim() } : s
      ))
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
    if (overlayRef.current && containerRef.current) {
      gsap.to(overlayRef.current, { opacity: 0, duration: 0.2 })
      gsap.to(containerRef.current, {
        y: '100%',
        duration: 0.3,
        ease: 'power2.in',
        onComplete: onClose
      })
    } else {
      onClose()
    }
  }

  const handleSessionClick = (sessionId: string) => {
    if (sessionId === currentSessionId) return
    handleClose()
    // 延迟切换以等待关闭动画完成
    setTimeout(() => {
      onSwitchSession(sessionId)
    }, 350)
  }

  const getAvatarSrc = () => {
    if (prompt.avatar) {
      return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
    }
    return null
  }

  return (
    <div className="chat-settings-overlay" ref={overlayRef} onClick={handleClose}>
      <div
        className="chat-settings-container"
        ref={containerRef}
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
              <div className="avatar-placeholder">
                {prompt.name.charAt(0).toUpperCase()}
              </div>
            )}
          </div>
          <div className="prompt-name">{prompt.name}</div>
          {prompt.description && (
            <div className="prompt-description">{prompt.description}</div>
          )}
        </div>

        {/* 当前聊天设置 */}
        <div className="chat-settings-section">
          <div className="section-title">当前聊天</div>
          <div className="current-chat-card">
            <div className="setting-row">
              <span className="setting-label">聊天标题</span>
              {editingTitle ? (
                <div className="title-edit-container">
                  <input
                    ref={titleInputRef}
                    type="text"
                    className="title-input"
                    value={titleInput}
                    onChange={(e) => setTitleInput(e.target.value)}
                    onKeyDown={handleTitleKeyDown}
                    placeholder="输入聊天标题"
                    disabled={savingTitle}
                  />
                  <div className="title-edit-actions">
                    <button
                      className="title-btn cancel"
                      onClick={handleCancelEdit}
                      disabled={savingTitle}
                    >
                      取消
                    </button>
                    <button
                      className="title-btn save"
                      onClick={handleSaveTitle}
                      disabled={!titleInput.trim() || savingTitle}
                    >
                      {savingTitle ? '保存中...' : '保存'}
                    </button>
                  </div>
                </div>
              ) : (
                <div className="setting-value-row" onClick={() => setEditingTitle(true)}>
                  <span className="setting-value">{currentSessionTitle || '未命名'}</span>
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
          <div className="section-title">聊天记录</div>
          <div className="sessions-card">
            {loading ? (
              <div className="sessions-loading">加载中...</div>
            ) : sessions.length === 0 ? (
              <div className="sessions-empty">暂无聊天记录</div>
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
                    {session.id === currentSessionId && (
                      <div className="session-current-badge">当前</div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default ChatSettings
