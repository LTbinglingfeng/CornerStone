import { useState, useEffect, useCallback } from 'react'
import type { ChatSession, Prompt } from '../types/chat'
import { getSessions, getPrompts, getPromptAvatarUrl, appendQueryParam } from '../services/api'
import { formatTime } from '../utils/time'
import ContextMenu from './ContextMenu'
import { deleteSession } from '../services/api'
import './ChatList.css'

interface ChatListProps {
  onSelectSession: (id: string, promptId?: string) => void
  searchQuery?: string
}

interface PromptWithLatestChat {
  prompt: Prompt
  latestSession: ChatSession
  sessionCount: number
}

interface MenuState {
  visible: boolean
  sessionId: string
  position: { x: number; y: number }
}

const ChatList: React.FC<ChatListProps> = ({ onSelectSession, searchQuery = '' }) => {
  const [promptsWithChats, setPromptsWithChats] = useState<PromptWithLatestChat[]>([])
  const [orphanSessions, setOrphanSessions] = useState<ChatSession[]>([])
  const [loading, setLoading] = useState(true)
  const [menuState, setMenuState] = useState<MenuState>({
    visible: false,
    sessionId: '',
    position: { x: 0, y: 0 },
  })

  useEffect(() => {
    loadData()
  }, [])

  const loadData = async () => {
    setLoading(true)
    const [sessions, prompts] = await Promise.all([
      getSessions(),
      getPrompts()
    ])

    // 按 prompt_id 分组聊天
    const sessionsByPrompt: Record<string, ChatSession[]> = {}
    const orphans: ChatSession[] = []

    sessions.forEach(session => {
      if (session.prompt_id) {
        if (!sessionsByPrompt[session.prompt_id]) {
          sessionsByPrompt[session.prompt_id] = []
        }
        sessionsByPrompt[session.prompt_id].push(session)
      } else {
        orphans.push(session)
      }
    })

    // 构建带有最新聊天的 prompt 列表
    const promptMap = new Map(prompts.map(p => [p.id, p]))
    const result: PromptWithLatestChat[] = []

    Object.entries(sessionsByPrompt).forEach(([promptId, promptSessions]) => {
      const prompt = promptMap.get(promptId)
      if (prompt && promptSessions.length > 0) {
        // 按更新时间排序，取最新的
        const sorted = promptSessions.sort((a, b) =>
          new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
        )
        result.push({
          prompt,
          latestSession: sorted[0],
          sessionCount: sorted.length
        })
      }
    })

    // 按最新聊天时间排序
    result.sort((a, b) =>
      new Date(b.latestSession.updated_at).getTime() - new Date(a.latestSession.updated_at).getTime()
    )

    // 孤儿聊天也按时间排序
    orphans.sort((a, b) =>
      new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    )

    setPromptsWithChats(result)
    setOrphanSessions(orphans)
    setLoading(false)
  }

  const handleLongPress = useCallback((sessionId: string, position: { x: number; y: number }) => {
    setMenuState({
      visible: true,
      sessionId,
      position,
    })
  }, [])

  const handleCloseMenu = useCallback(() => {
    setMenuState((prev) => ({ ...prev, visible: false }))
  }, [])

  const handleDeleteSession = useCallback(async () => {
    const success = await deleteSession(menuState.sessionId)
    if (success) {
      loadData()
    }
  }, [menuState.sessionId])

  const getAvatarUrl = (prompt: Prompt) => {
    if (prompt.avatar) {
      return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
    }
    return null
  }

  const getAvatarColors = (name: string) => {
    const colors = [
      '#4a90d9', '#7ed321', '#bd10e0', '#f5a623',
      '#50e3c2', '#9013fe', '#417505', '#2b5797'
    ]
    const firstChar = name.charAt(0) || '?'
    const colorIndex = firstChar.charCodeAt(0) % colors.length
    return colors[colorIndex]
  }

  const normalizedQuery = searchQuery.trim().toLowerCase()
  const hasQuery = normalizedQuery.length > 0

  const filteredPromptsWithChats = hasQuery
    ? promptsWithChats.filter(({ prompt, latestSession }) => {
      const haystack = `${prompt.name} ${prompt.description || ''} ${latestSession.title}`.toLowerCase()
      return haystack.includes(normalizedQuery)
    })
    : promptsWithChats

  const filteredOrphanSessions = hasQuery
    ? orphanSessions.filter((session) => session.title.toLowerCase().includes(normalizedQuery))
    : orphanSessions

  if (loading) {
    return (
      <div className="chat-list">
        <div className="chat-list-empty">加载中...</div>
      </div>
    )
  }

  if (filteredPromptsWithChats.length === 0 && filteredOrphanSessions.length === 0) {
    return (
      <div className="chat-list">
        <div className="chat-list-empty">
          {hasQuery ? '未找到匹配的会话' : '暂无会话，点击右上角 + 创建新会话'}
        </div>
      </div>
    )
  }

  return (
    <div className="chat-list">
      {/* 有 Prompt 的聊天（按角色显示） */}
      {filteredPromptsWithChats.map(({ prompt, latestSession, sessionCount }) => (
        <div
          key={prompt.id}
          className="chat-item prompt-chat-item"
          onClick={() => onSelectSession(latestSession.id, prompt.id)}
          onContextMenu={(e) => {
            e.preventDefault()
            handleLongPress(latestSession.id, { x: e.clientX, y: e.clientY })
          }}
        >
          <div className="avatar-container">
            {getAvatarUrl(prompt) ? (
              <img src={getAvatarUrl(prompt)!} alt={prompt.name} className="avatar-img" />
            ) : (
              <div className="avatar" style={{ backgroundColor: getAvatarColors(prompt.name) }}>
                {prompt.name.charAt(0).toUpperCase()}
              </div>
            )}
          </div>
          <div className="chat-content">
            <div className="chat-header">
              <span className="chat-name">{prompt.name}</span>
              <span className="chat-time">{formatTime(latestSession.updated_at)}</span>
            </div>
            <div className="chat-message">
              {latestSession.title}
              {sessionCount > 1 && (
                <span className="session-count">{sessionCount}条对话</span>
              )}
            </div>
          </div>
        </div>
      ))}

      {/* 无 Prompt 的孤儿聊天 */}
      {filteredOrphanSessions.length > 0 && (
        <>
          {filteredPromptsWithChats.length > 0 && (
            <div className="orphan-section-title">其他对话</div>
          )}
          {filteredOrphanSessions.map((session) => (
            <div
              key={session.id}
              className="chat-item"
              onClick={() => onSelectSession(session.id)}
              onContextMenu={(e) => {
                e.preventDefault()
                handleLongPress(session.id, { x: e.clientX, y: e.clientY })
              }}
            >
              <div className="avatar-container">
                <div className="avatar" style={{ backgroundColor: getAvatarColors(session.title) }}>
                  {session.title.charAt(0)}
                </div>
              </div>
              <div className="chat-content">
                <div className="chat-header">
                  <span className="chat-name">{session.title}</span>
                  <span className="chat-time">{formatTime(session.updated_at)}</span>
                </div>
                <div className="chat-message">点击查看对话</div>
              </div>
            </div>
          ))}
        </>
      )}

      {menuState.visible && (
        <ContextMenu
          position={menuState.position}
          onClose={handleCloseMenu}
          items={[
            {
              label: '删除会话',
              onClick: handleDeleteSession,
              danger: true,
            },
          ]}
        />
      )}
    </div>
  )
}

export default ChatList
