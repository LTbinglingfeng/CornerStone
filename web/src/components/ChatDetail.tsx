import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import type { ChatMessage, ChatRecord, Prompt, UserInfo, ToolCall, RedPacketParams } from '../types/chat'
import { getSession, sendMessage, getPrompt, getUserInfo, getPromptAvatarUrl, getUserAvatarUrl } from '../services/api'
import ChatSettings from './ChatSettings'
import './ChatDetail.css'

interface ChatDetailProps {
  sessionId: string
  promptId?: string
  onBack: () => void
  onSwitchSession?: (sessionId: string, promptId?: string) => void
}

const ChatDetail: React.FC<ChatDetailProps> = ({ sessionId, promptId, onBack, onSwitchSession }) => {
  const [session, setSession] = useState<ChatRecord | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [inputText, setInputText] = useState('')
  const [loading, setLoading] = useState(false)
  const [sending, setSending] = useState(false)
  const [prompt, setPrompt] = useState<Prompt | null>(null)
  const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
  const [showSettings, setShowSettings] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const activeRequestRef = useRef<AbortController | null>(null)

  useEffect(() => {
    activeRequestRef.current?.abort()
    activeRequestRef.current = null
    setSending(false)
    if (containerRef.current) {
      gsap.fromTo(
        containerRef.current,
        { x: '100%' },
        { x: '0%', duration: 0.3, ease: 'power2.out' }
      )
    }
    loadSession()
  }, [sessionId])

  useEffect(() => {
    return () => {
      activeRequestRef.current?.abort()
    }
  }, [])

  useEffect(() => {
    scrollToBottom()
  }, [messages])

  const loadSession = async () => {
    setLoading(true)
    const data = await getSession(sessionId)
    if (data) {
      setSession(data)
      setMessages(data.messages || [])

      // 加载 prompt 信息（使用传入的 promptId 或从 session 中获取）
      const pId = promptId || data.prompt_id
      if (pId) {
        const promptData = await getPrompt(pId)
        if (promptData) {
          setPrompt(promptData)
        }
      }
    }

    // 加载用户信息
    const user = await getUserInfo()
    if (user) {
      setUserInfo(user)
    }

    setLoading(false)
  }

  const scrollToBottom = () => {
    if (messageListRef.current) {
      messageListRef.current.scrollTop = messageListRef.current.scrollHeight
    }
  }

  const handleBack = () => {
    activeRequestRef.current?.abort()
    activeRequestRef.current = null
    if (containerRef.current) {
      gsap.to(containerRef.current, {
        x: '100%',
        duration: 0.3,
        ease: 'power2.in',
        onComplete: onBack
      })
    } else {
      onBack()
    }
  }

  const handleSend = async () => {
    if (!inputText.trim() || sending) return

    const userMessage: ChatMessage = {
      role: 'user',
      content: inputText.trim(),
      timestamp: new Date().toISOString()
    }

    setMessages(prev => [...prev, userMessage])
    setInputText('')
    setSending(true)

    if (textareaRef.current) {
      textareaRef.current.style.height = '36px'
    }

    let streamingAssistantTimestamp: string | null = null

    try {
      activeRequestRef.current?.abort()
      const abortController = new AbortController()
      activeRequestRef.current = abortController

      const allMessages = [...messages, userMessage]
      const response = await sendMessage(sessionId, allMessages, {
        promptId,
        signal: abortController.signal,
      })

      // 检查响应类型，判断是流式还是非流式
      const contentType = response.headers.get('Content-Type') || ''
      const isStreaming = contentType.includes('text/event-stream')

      if (isStreaming) {
        // 流式响应处理
        const reader = response.body?.getReader()
        const decoder = new TextDecoder()

        if (!reader) {
          throw new Error('No response body')
        }

        let assistantContent = ''
        let reasoningContent = ''
        // 用于收集流式 tool_calls
        const toolCallsMap: Map<number, { id: string; type: string; name: string; arguments: string }> = new Map()
        const assistantTimestamp = new Date().toISOString()
        streamingAssistantTimestamp = assistantTimestamp
        const assistantMessage: ChatMessage = {
          role: 'assistant',
          content: '',
          timestamp: assistantTimestamp,
        }

        setMessages(prev => [...prev, assistantMessage])

        let buffer = ''
        let sseDone = false

        const applyStreamUpdate = () => {
          // 将 toolCallsMap 转换为 ToolCall 数组
          const toolCalls: ToolCall[] = Array.from(toolCallsMap.values()).map(tc => ({
            id: tc.id,
            type: tc.type,
            function: {
              name: tc.name,
              arguments: tc.arguments,
            },
          }))

          setMessages(prev => {
            const next = [...prev]
            for (let i = next.length - 1; i >= 0; i--) {
              const msg = next[i]
              if (msg.role === 'assistant' && msg.timestamp === assistantTimestamp) {
                next[i] = {
                  ...msg,
                  content: assistantContent,
                  ...(reasoningContent ? { reasoning_content: reasoningContent } : {}),
                  ...(toolCalls.length > 0 ? { tool_calls: toolCalls } : {}),
                }
                break
              }
            }
            return next
          })
        }

        while (!sseDone) {
          const { done, value } = await reader.read()
          if (done) break

          buffer += decoder.decode(value, { stream: true })
          const events = buffer.split(/\r?\n\r?\n/)
          buffer = events.pop() || ''

          for (const event of events) {
            const data = event
              .split(/\r?\n/)
              .filter((line) => line.startsWith('data:'))
              .map((line) => line.replace(/^data:\s?/, ''))
              .join('\n')
              .trim()

            if (!data) continue
            if (data === '[DONE]') {
              sseDone = true
              break
            }

            const parsed = JSON.parse(data) as {
              session_id?: string
              error?: string
              choices?: Array<{
                delta?: {
                  content?: string
                  reasoning_content?: string
                  tool_calls?: Array<{
                    index: number
                    id?: string
                    type?: string
                    function?: { name?: string; arguments?: string }
                  }>
                }
              }>
            }

            if (parsed.session_id && !parsed.choices) continue
            if (parsed.error) throw new Error(parsed.error)

            const delta = parsed.choices?.[0]?.delta
            if (!delta) continue

            if (delta.content) assistantContent += delta.content
            if (delta.reasoning_content) reasoningContent += delta.reasoning_content

            // 处理流式 tool_calls
            if (delta.tool_calls) {
              for (const tc of delta.tool_calls) {
                const existing = toolCallsMap.get(tc.index)
                if (existing) {
                  // 追加 arguments
                  if (tc.function?.arguments) {
                    existing.arguments += tc.function.arguments
                  }
                } else {
                  // 新建 tool_call
                  toolCallsMap.set(tc.index, {
                    id: tc.id || '',
                    type: tc.type || 'function',
                    name: tc.function?.name || '',
                    arguments: tc.function?.arguments || '',
                  })
                }
              }
            }

            applyStreamUpdate()
          }
        }
      } else {
        // 非流式响应处理
        const result = await response.json()

        if (result.success && result.data?.response?.choices?.[0]?.message) {
          const msg = result.data.response.choices[0].message
          const assistantMessage: ChatMessage = {
            role: 'assistant',
            content: msg.content || '',
            reasoning_content: msg.reasoning_content,
            tool_calls: msg.tool_calls,
            timestamp: new Date().toISOString()
          }
          setMessages(prev => [...prev, assistantMessage])
        } else if (result.success === false && result.error) {
          throw new Error(result.error)
        } else {
          throw new Error('Invalid response format')
        }
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        return
      }
      if (error instanceof Error && (error as Error & { name?: string }).name === 'AbortError') {
        return
      }

      const message = error instanceof Error && error.message ? error.message : '发送失败，请重试'
      setMessages(prev => {
        if (streamingAssistantTimestamp) {
          const next = [...prev]
          for (let i = next.length - 1; i >= 0; i--) {
            const msg = next[i]
            if (msg.role === 'assistant' && msg.timestamp === streamingAssistantTimestamp) {
              next[i] = { ...msg, content: message }
              return next
            }
          }
        }

        return [
          ...prev,
          {
            role: 'assistant',
            content: message,
            timestamp: new Date().toISOString(),
          },
        ]
      })
    } finally {
      setSending(false)
      activeRequestRef.current = null
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInputText(e.target.value)
    const textarea = e.target
    textarea.style.height = '36px'
    textarea.style.height = Math.min(textarea.scrollHeight, 120) + 'px'
  }

  // 获取用户头像 URL
  const getUserAvatarSrc = () => {
    if (userInfo?.avatar) {
      return getUserAvatarUrl() + `?t=${new Date(userInfo.updated_at).getTime()}`
    }
    return null
  }

  // 获取 prompt 头像 URL
  const getPromptAvatarSrc = () => {
    if (prompt?.avatar) {
      return getPromptAvatarUrl(prompt.id) + `?t=${new Date(prompt.updated_at).getTime()}`
    }
    return null
  }

  // 渲染头像
  const renderAvatar = (role: string) => {
    if (role === 'user') {
      const avatarSrc = getUserAvatarSrc()
      return (
        <div className="message-avatar">
          {avatarSrc ? (
            <img src={avatarSrc} alt="用户" />
          ) : (
            <div className="avatar-placeholder user">
              {userInfo?.username?.charAt(0)?.toUpperCase() || 'U'}
            </div>
          )}
        </div>
      )
    } else {
      const avatarSrc = getPromptAvatarSrc()
      return (
        <div className="message-avatar">
          {avatarSrc ? (
            <img src={avatarSrc} alt="AI" />
          ) : (
            <div className="avatar-placeholder assistant">
              {prompt?.name?.charAt(0)?.toUpperCase() || 'A'}
            </div>
          )}
        </div>
      )
    }
  }

  // 渲染红包卡片
  const renderRedPacket = (toolCall: ToolCall) => {
    if (toolCall.function.name !== 'send_red_packet') return null

    try {
      const params: RedPacketParams = JSON.parse(toolCall.function.arguments)
      return (
        <div className="red-packet-card" key={toolCall.id}>
          <div className="red-packet-header">
            <div className="red-packet-icon">
              <svg viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1.41 16.09V20h-2.67v-1.93c-1.71-.36-3.16-1.46-3.27-3.4h1.96c.1 1.05.82 1.87 2.65 1.87 1.96 0 2.4-.98 2.4-1.59 0-.83-.44-1.61-2.67-2.14-2.48-.6-4.18-1.62-4.18-3.67 0-1.72 1.39-2.84 3.11-3.21V4h2.67v1.95c1.86.45 2.79 1.86 2.85 3.39H14.3c-.05-1.11-.64-1.87-2.22-1.87-1.5 0-2.4.68-2.4 1.64 0 .84.65 1.39 2.67 1.91s4.18 1.39 4.18 3.91c-.01 1.83-1.38 2.83-3.12 3.16z"/>
              </svg>
            </div>
            <div className="red-packet-title">红包</div>
          </div>
          <div className="red-packet-body">
            <div className="red-packet-amount">¥{params.amount.toFixed(2)}</div>
            <div className="red-packet-message">{params.message}</div>
          </div>
          <div className="red-packet-footer">
            点击领取红包
          </div>
        </div>
      )
    } catch {
      return null
    }
  }

  // 渲染消息内容（包含文本和工具调用）
  const renderMessageContent = (message: ChatMessage, index: number) => {
    const hasToolCalls = message.tool_calls && message.tool_calls.length > 0
    const hasContent = message.content && message.content.trim() !== ''

    return (
      <>
        {hasContent && <div className="message-text">{message.content}</div>}
        {!hasContent && !hasToolCalls && (
          sending && index === messages.length - 1 && message.role === 'assistant' ? (
            <div className="message-loading">
              <span></span>
              <span></span>
              <span></span>
            </div>
          ) : ''
        )}
        {hasToolCalls && (
          <div className="tool-calls-container">
            {message.tool_calls!.map(tc => renderRedPacket(tc))}
          </div>
        )}
      </>
    )
  }

  return (
    <div className="chat-detail" ref={containerRef}>
      <div className="chat-detail-header">
        <button className="back-button" onClick={handleBack}>
          <svg viewBox="0 0 24 24">
            <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
          </svg>
        </button>
        <div className="chat-detail-title">
          {session?.title || '对话'}
        </div>
        {prompt && (
          <button className="settings-button" onClick={() => setShowSettings(true)}>
            <svg viewBox="0 0 24 24">
              <path d="M19.14,12.94c0.04-0.3,0.06-0.61,0.06-0.94c0-0.32-0.02-0.64-0.07-0.94l2.03-1.58c0.18-0.14,0.23-0.41,0.12-0.61 l-1.92-3.32c-0.12-0.22-0.37-0.29-0.59-0.22l-2.39,0.96c-0.5-0.38-1.03-0.7-1.62-0.94L14.4,2.81c-0.04-0.24-0.24-0.41-0.48-0.41 h-3.84c-0.24,0-0.43,0.17-0.47,0.41L9.25,5.35C8.66,5.59,8.12,5.92,7.63,6.29L5.24,5.33c-0.22-0.08-0.47,0-0.59,0.22L2.74,8.87 C2.62,9.08,2.66,9.34,2.86,9.48l2.03,1.58C4.84,11.36,4.8,11.69,4.8,12s0.02,0.64,0.07,0.94l-2.03,1.58 c-0.18,0.14-0.23,0.41-0.12,0.61l1.92,3.32c0.12,0.22,0.37,0.29,0.59,0.22l2.39-0.96c0.5,0.38,1.03,0.7,1.62,0.94l0.36,2.54 c0.05,0.24,0.24,0.41,0.48,0.41h3.84c0.24,0,0.44-0.17,0.47-0.41l0.36-2.54c0.59-0.24,1.13-0.56,1.62-0.94l2.39,0.96 c0.22,0.08,0.47,0,0.59-0.22l1.92-3.32c0.12-0.22,0.07-0.47-0.12-0.61L19.14,12.94z M12,15.6c-1.98,0-3.6-1.62-3.6-3.6 s1.62-3.6,3.6-3.6s3.6,1.62,3.6,3.6S13.98,15.6,12,15.6z"/>
            </svg>
          </button>
        )}
      </div>

      <div className="message-list" ref={messageListRef}>
        {loading ? (
          <div className="empty-messages">加载中...</div>
        ) : messages.length === 0 ? (
          <div className="empty-messages">开始新的对话</div>
        ) : (
          messages.map((message, index) => (
            <div key={index} className={`message-item ${message.role}`}>
              {message.role === 'assistant' && renderAvatar(message.role)}
              <div className="message-bubble">
                {renderMessageContent(message, index)}
              </div>
              {message.role === 'user' && renderAvatar(message.role)}
            </div>
          ))
        )}
      </div>

      <div className="chat-input-area">
        <textarea
          ref={textareaRef}
          className="chat-input"
          placeholder="输入消息..."
          value={inputText}
          onChange={handleInputChange}
          onKeyDown={handleKeyDown}
          rows={1}
        />
        <button
          className="send-button"
          onClick={handleSend}
          disabled={!inputText.trim() || sending}
        >
          <svg viewBox="0 0 24 24">
            <path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" />
          </svg>
        </button>
      </div>

      {/* 设置面板 */}
      {showSettings && prompt && (
        <ChatSettings
          prompt={prompt}
          currentSessionId={sessionId}
          currentSessionTitle={session?.title}
          onClose={() => setShowSettings(false)}
          onSwitchSession={(newSessionId) => {
            setShowSettings(false)
            if (onSwitchSession) {
              onSwitchSession(newSessionId, prompt.id)
            }
          }}
          onTitleUpdated={(newTitle) => {
            setSession(prev => prev ? { ...prev, title: newTitle } : prev)
          }}
        />
      )}
    </div>
  )
}

export default ChatDetail
