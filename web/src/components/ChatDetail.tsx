import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import type { ChatMessage, ChatRecord, Prompt, UserInfo, ToolCall, RedPacketParams, PatParams } from '../types/chat'
import { getSession, sendMessage, getPrompt, getUserInfo, getPromptAvatarUrl, getUserAvatarUrl, uploadChatImage, getChatImageUrl, getActiveProvider, appendQueryParam } from '../services/api'
import ChatSettings from './ChatSettings'
import './ChatDetail.css'

interface ChatDetailProps {
  sessionId: string
  promptId?: string
  onBack: () => void
  onSwitchSession?: (sessionId: string, promptId?: string) => void
}

type DisplayItem =
  | { key: string; role: string; type: 'text'; message: ChatMessage }
  | { key: string; role: string; type: 'loading'; message: ChatMessage }
  | { key: string; role: string; type: 'red-packet'; message: ChatMessage; toolCall: ToolCall }
  | { key: string; role: string; type: 'pat-banner'; message: ChatMessage; toolCall: ToolCall }

const assistantMessageSplitToken = '→'

const splitAssistantMessageContent = (content: string): string[] => {
  if (!content) return []
  if (!content.includes(assistantMessageSplitToken)) {
    return content.trim() ? [content] : []
  }

  return content
    .split(assistantMessageSplitToken)
    .map(part => part.trim())
    .filter(part => part !== '')
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
  const [pendingImages, setPendingImages] = useState<string[]>([])
  const [uploadingImage, setUploadingImage] = useState(false)
  const [imageCapable, setImageCapable] = useState(false)

  // Red Packet State
  const [activeRedPacket, setActiveRedPacket] = useState<RedPacketParams | null>(null)
  const [packetStep, setPacketStep] = useState<'idle' | 'opening' | 'opened'>('idle')

  const containerRef = useRef<HTMLDivElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const activeRequestRef = useRef<AbortController | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    activeRequestRef.current?.abort()
    activeRequestRef.current = null
    setSending(false)
    setActiveRedPacket(null)
    setPacketStep('idle')
    setPendingImages([])
    setUploadingImage(false)
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
    if (loading) return
    scrollToBottom()
  }, [messages, loading])

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

    const provider = await getActiveProvider()
    setImageCapable(!!provider?.image_capable)

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
    const trimmedText = inputText.trim()
    const hasImages = pendingImages.length > 0
    if ((!trimmedText && !hasImages) || sending || uploadingImage) return
    if (hasImages && !imageCapable) {
      alert('当前模型不支持图片输入')
      return
    }

    const userMessage: ChatMessage = {
      role: 'user',
      content: trimmedText,
      timestamp: new Date().toISOString(),
      ...(hasImages ? { image_paths: pendingImages } : {}),
    }

    setMessages(prev => [...prev, userMessage])
    setInputText('')
    setPendingImages([])
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

  const handleUploadClick = () => {
    if (!imageCapable || uploadingImage) return
    fileInputRef.current?.click()
  }

  const handleImageChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files ? Array.from(e.target.files) : []
    if (files.length === 0) return
    if (!imageCapable) {
      e.target.value = ''
      return
    }

    setUploadingImage(true)
    const uploadedPaths: string[] = []
    for (const file of files) {
      const savedPath = await uploadChatImage(file)
      if (savedPath) {
        uploadedPaths.push(savedPath)
      }
    }

    if (uploadedPaths.length > 0) {
      setPendingImages(prev => [...prev, ...uploadedPaths])
    } else {
      alert('图片上传失败')
    }

    setUploadingImage(false)
    e.target.value = ''
  }

  const handleRemovePendingImage = (index: number) => {
    setPendingImages(prev => prev.filter((_, i) => i !== index))
  }

  // 获取用户头像 URL
  const getUserAvatarSrc = () => {
    if (userInfo?.avatar) {
      return appendQueryParam(getUserAvatarUrl(), 't', new Date(userInfo.updated_at).getTime())
    }
    return null
  }

  // 获取 prompt 头像 URL
  const getPromptAvatarSrc = () => {
    if (prompt?.avatar) {
      return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
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

  const renderMessageImages = (message: ChatMessage) => {
    if (!message.image_paths || message.image_paths.length === 0) return null
    return (
      <div className="message-images">
        {message.image_paths.map((imagePath, index) => (
          <img
            key={`${message.timestamp}-image-${index}`}
            src={getChatImageUrl(imagePath)}
            alt="聊天图片"
            className="message-image"
          />
        ))}
      </div>
    )
  }

  // 渲染红包卡片
  const renderRedPacket = (toolCall: ToolCall) => {
    if (toolCall.function.name !== 'send_red_packet') return null

    try {
      const params: RedPacketParams = JSON.parse(toolCall.function.arguments)
      return (
        <div 
          className="red-packet-bubble" 
          onClick={() => {
            setActiveRedPacket(params)
            setPacketStep('idle')
          }}
        >
          <div className="rp-content">
            <div className="rp-icon-wrapper">
              <svg viewBox="0 0 40 40" className="rp-icon">
                 <path d="M35.5,14.5c0-1.6-0.8-3-2.1-3.9l-10-6.7c-2.1-1.4-4.8-1.4-6.9,0l-10,6.7C5.3,11.5,4.5,12.9,4.5,14.5v16c0,2.5,2,4.5,4.5,4.5h22c2.5,0,4.5-2,4.5-4.5V14.5z M20,9.5l8.9,6L20,21.4L11.1,15.5L20,9.5z M9,31v-8.8l7.2,4.8L9,31z M20,25.6l-2.4-1.6l-2.4,3.2c-0.9,1.2-2.3,1.9-3.7,1.9h-1.3v2H20V25.6z M31,31h-9.8v-5.5h1.3c1.5,0,2.8-0.7,3.7-1.9l-2.4-3.2l-2.4,1.6l3.8,2.5L31,22.2V31z" fill="var(--red-packet-header-text)"/>
              </svg>
            </div>
            <div className="rp-text">
              <div className="rp-title">{params.message || '恭喜发财，大吉大利'}</div>
              <div className="rp-status">领取红包</div>
            </div>
          </div>
          <div className="rp-footer">
            微信红包
          </div>
        </div>
      )
    } catch {
      return null
    }
  }

  const renderPatBanner = (toolCall: ToolCall) => {
    if (toolCall.function.name !== 'send_pat') return null

    try {
      const params: PatParams = JSON.parse(toolCall.function.arguments || '{}')
      const name = (typeof params.name === 'string' ? params.name.trim() : '') || prompt?.name || 'AI Assistant'
      const target = (typeof params.target === 'string' ? params.target.trim() : '') || '我'

      return (
        <div className="pat-banner">
          <span className="pat-banner-text">"{name}"拍了拍{target}</span>
        </div>
      )
    } catch {
      return null
    }
  }

  const handleOpenPacket = () => {
    setPacketStep('opening')
    setTimeout(() => {
      setPacketStep('opened')
    }, 1000)
  }

  const buildDisplayItems = (): DisplayItem[] => {
    const items: DisplayItem[] = []
    messages.forEach((message, index) => {
      const hasImages = !!(message.image_paths && message.image_paths.length > 0)
      const toolCalls = message.tool_calls || []
      const supportedCalls = toolCalls.filter(tc => tc.function.name === 'send_red_packet' || tc.function.name === 'send_pat')

      const isAssistant = message.role === 'assistant'
      const assistantSegments = isAssistant ? splitAssistantMessageContent(message.content) : []
      const hasText = isAssistant ? assistantSegments.length > 0 : message.content.trim() !== ''
      const hasContent = hasText || hasImages

      if (hasContent) {
        if (isAssistant) {
          if (assistantSegments.length > 0) {
            assistantSegments.forEach((segment, segmentIndex) => {
              const segmentMessage: ChatMessage = {
                ...message,
                content: segment,
                ...(segmentIndex === 0 ? {} : { image_paths: undefined }),
              }
              items.push({
                key: `${message.timestamp}-text-${segmentIndex}`,
                role: message.role,
                type: 'text',
                message: segmentMessage,
              })
            })
          } else {
            items.push({
              key: `${message.timestamp}-text-0`,
              role: message.role,
              type: 'text',
              message: { ...message, content: '' },
            })
          }
        } else {
          items.push({
            key: `${message.timestamp}-text`,
            role: message.role,
            type: 'text',
            message,
          })
        }
      }

      supportedCalls.forEach((toolCall, toolIndex) => {
        if (toolCall.function.name === 'send_red_packet') {
          items.push({
            key: `${message.timestamp}-rp-${toolCall.id || toolIndex}`,
            role: message.role,
            type: 'red-packet',
            message,
            toolCall,
          })
        }
        if (toolCall.function.name === 'send_pat') {
          items.push({
            key: `${message.timestamp}-pat-${toolCall.id || toolIndex}`,
            role: message.role,
            type: 'pat-banner',
            message,
            toolCall,
          })
        }
      })

      if (!hasContent && supportedCalls.length === 0) {
        const shouldShowLoading = sending && index === messages.length - 1 && message.role === 'assistant'
        if (shouldShowLoading) {
          items.push({
            key: `${message.timestamp}-loading`,
            role: message.role,
            type: 'loading',
            message,
          })
        }
      }
    })
    return items
  }

  const renderMessageBubbleContent = (item: DisplayItem) => {
    if (item.type === 'loading') {
      return (
        <div className="message-loading">
          <span></span>
          <span></span>
          <span></span>
        </div>
      )
    }
    const images = renderMessageImages(item.message)
    const hasText = item.message.content && item.message.content.trim() !== ''
    return (
      <div className="message-content">
        {images}
        {hasText && <div className="message-text">{item.message.content}</div>}
      </div>
    )
  }

  const displayItems = buildDisplayItems()
  const canSend = (inputText.trim() !== '' || pendingImages.length > 0) && !sending && !uploadingImage

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
        ) : displayItems.length === 0 ? (
          <div className="empty-messages">开始新的对话</div>
        ) : (
          displayItems.map((item) => {
            const isRedPacket = item.type === 'red-packet'
            const isPatBanner = item.type === 'pat-banner'
            if (isPatBanner) {
              const banner = renderPatBanner(item.toolCall)
              if (!banner) return null
              return (
                <div key={item.key} className="message-item pat-banner-item">
                  {banner}
                </div>
              )
            }

            const content = isRedPacket ? (
              renderRedPacket(item.toolCall)
            ) : (
              <div className="message-bubble">
                {renderMessageBubbleContent(item)}
              </div>
            )

            return (
              <div key={item.key} className={`message-item ${item.role} ${isRedPacket ? 'red-packet-item' : ''}`}>
                {item.role === 'assistant' && renderAvatar(item.role)}
                {content}
                {item.role === 'user' && renderAvatar(item.role)}
              </div>
            )
          })
        )}
      </div>

      {pendingImages.length > 0 && (
        <div className="pending-image-list">
          {pendingImages.map((imagePath, index) => (
            <div key={`${imagePath}-${index}`} className="pending-image-item">
              <img
                src={getChatImageUrl(imagePath)}
                alt="待发送图片"
              />
              <button
                type="button"
                className="pending-image-remove"
                onClick={() => handleRemovePendingImage(index)}
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}

      <div className="chat-input-area">
        <button
          className="upload-button"
          onClick={handleUploadClick}
          disabled={!imageCapable || uploadingImage}
          title={imageCapable ? '上传图片' : '当前模型不支持图片输入'}
        >
          <svg viewBox="0 0 24 24">
            <path d="M19 7h-3V5c0-1.1-.9-2-2-2h-4c-1.1 0-2 .9-2 2v2H5c-1.1 0-2 .9-2 2v9c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V9c0-1.1-.9-2-2-2zm-9 0V5h4v2h-4zm7 13H5V9h14v11zM7 18l3-4 2 3 3-4 2 5H7z" />
          </svg>
        </button>
        <input
          ref={fileInputRef}
          className="image-input"
          type="file"
          accept="image/*"
          multiple
          onChange={handleImageChange}
        />
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
          disabled={!canSend}
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
          onExitChat={handleBack}
        />
      )}

      {/* Red Packet Modal */}
      {activeRedPacket && (
        <div className="rp-modal-overlay">
          <div className={`rp-modal ${packetStep === 'opened' ? 'opened' : ''}`}>
            <button className="rp-close-btn" onClick={() => setActiveRedPacket(null)}>×</button>
            
            {packetStep !== 'opened' ? (
              <div className="rp-modal-front">
                <div className="rp-modal-top">
                  <div className="rp-sender-row">
                    {getPromptAvatarSrc() ? (
                        <img src={getPromptAvatarSrc()!} className="rp-avatar-img" alt="avatar" />
                    ) : (
                        <div className="rp-avatar-placeholder">
                            {prompt?.name?.charAt(0)?.toUpperCase() || 'A'}
                        </div>
                    )}
                    <span className="rp-sender-name">{prompt?.name || 'AI Assistant'}</span>
                  </div>
                  <div className="rp-wishing">
                    {activeRedPacket.message || '恭喜发财，大吉大利'}
                  </div>
                </div>
                <div className="rp-modal-open-btn-wrapper">
                   <button 
                     className={`rp-open-btn ${packetStep === 'opening' ? 'opening' : ''}`}
                     onClick={handleOpenPacket}
                   >
                     開
                   </button>
                </div>
              </div>
            ) : (
              <div className="rp-modal-result">
                <div className="rp-result-header">
                  <div className="rp-result-top-bg"></div>
                  <div className="rp-sender-row small">
                    {getPromptAvatarSrc() ? (
                        <img src={getPromptAvatarSrc()!} className="rp-avatar-img small" alt="avatar" />
                    ) : (
                        <div className="rp-avatar-placeholder small">
                            {prompt?.name?.charAt(0)?.toUpperCase() || 'A'}
                        </div>
                    )}
                    <span className="rp-sender-name dark">{prompt?.name || 'AI Assistant'}的红包</span>
                  </div>
                  <div className="rp-wishing dark">
                    {activeRedPacket.message || '恭喜发财，大吉大利'}
                  </div>
                </div>
                
                <div className="rp-result-amount">
                  <span className="rp-currency">¥</span>
                  <span className="rp-num">{activeRedPacket.amount.toFixed(2)}</span>
                </div>

                <div className="rp-result-footer">
                  已存入零钱，可直接使用
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export default ChatDetail
