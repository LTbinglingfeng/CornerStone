import { useState, useEffect, useRef, useMemo } from 'react'
import { createPortal } from 'react-dom'
import { gsap } from 'gsap'
import type { ChatMessage, ChatRecord, Prompt, UserInfo, ToolCall, RedPacketParams, PatParams } from '../types/chat'
import { getSession, sendMessage, getPrompt, getUserInfo, getPromptAvatarUrl, getUserAvatarUrl, uploadChatImage, getChatImageUrl, getActiveProvider, appendQueryParam, updateSessionMessage, deleteSessionMessage, recallSessionMessage, openSessionRedPacket } from '../services/api'
import { getReplyWaitWindowConfig } from '../utils/replyWaitWindow'
import ChatSettings from './ChatSettings'
import ContextMenu, { type MenuItem } from './ContextMenu'
import './ChatDetail.css'

interface ChatDetailProps {
  sessionId: string
  promptId?: string
  onBack: () => void
  onSwitchSession?: (sessionId: string, promptId?: string) => void
}

type DisplayItem =
  | { key: string; role: string; type: 'text'; message: ChatMessage; messageIndex: number }
  | { key: string; role: string; type: 'red-packet'; message: ChatMessage; toolCall: ToolCall; messageIndex: number }
  | { key: string; role: string; type: 'red-packet-received-banner'; message: ChatMessage; toolCall: ToolCall; messageIndex: number }
  | { key: string; role: string; type: 'pat-banner'; message: ChatMessage; toolCall: ToolCall; messageIndex: number }
  | { key: string; role: string; type: 'recall-banner'; message: ChatMessage; messageIndex: number }

const assistantMessageSplitToken = '→'
const assistantBubbleIntervalMs = 1500
const quotePrefixCandidates = ['引用的信息:', '引用的信息：']
const recalledMessageSuffix = '(已撤回)'

const isRecalledMessage = (message: ChatMessage): boolean => {
  if (message.role !== 'user') return false
  return message.content.trimEnd().endsWith(recalledMessageSuffix)
}

const parseQuotedMessageContent = (content: string): { quoteLine: string; text: string } | null => {
  if (!content) return null
  for (const prefix of quotePrefixCandidates) {
    if (!content.startsWith(prefix)) continue
    const payload = content.slice(prefix.length).trimStart()
    const newlineIndex = payload.indexOf('\n')
    if (newlineIndex === -1) {
      return { quoteLine: payload.trim(), text: '' }
    }
    return {
      quoteLine: payload.slice(0, newlineIndex).trim(),
      text: payload.slice(newlineIndex + 1),
    }
  }
  return null
}

const buildQuotedOutgoingContent = (quoteLine: string, text: string): string => {
  const header = `引用的信息: ${quoteLine}`
  if (text.trim() === '') return header
  return `${header}\n${text}`
}

const normalizeAssistantContent = (content: string): string => {
  if (!content) return ''
  const withoutBlocks = content.replace(/<think[^>]*>[\s\S]*?<\/think\s*>/gi, '')
  const lower = withoutBlocks.toLowerCase()
  const openIndex = lower.indexOf('<think')
  if (openIndex !== -1) {
    return withoutBlocks.slice(0, openIndex)
  }
  return withoutBlocks.replace(/<\/think\s*>/gi, '')
}

type QuoteDraft = {
  line: string
}

type MessageMenuState = {
  position: { x: number; y: number }
  messageIndex: number
  message: ChatMessage
}

type MessageEditState = {
  messageIndex: number
  quoteLine: string | null
  text: string
}

type SelectTextState = {
  text: string
}

type ActiveRedPacketState = {
  params: RedPacketParams
  packetKey: string
  senderRole: 'user' | 'assistant'
  senderName: string
  senderAvatarSrc: string | null
}

const splitAssistantMessageContent = (content: string): string[] => {
  const normalized = normalizeAssistantContent(content)
  if (!normalized) return []
  if (!normalized.includes(assistantMessageSplitToken)) {
    return normalized.trim() ? [normalized] : []
  }

  return normalized
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
  const [assistantResponseDone, setAssistantResponseDone] = useState(false)
  const [revealingAssistantTimestamp, setRevealingAssistantTimestamp] = useState<string | null>(null)
  const [assistantVisibleSegments, setAssistantVisibleSegments] = useState(0)
  const [prompt, setPrompt] = useState<Prompt | null>(null)
  const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
  const [showSettings, setShowSettings] = useState(false)
  const [pendingImages, setPendingImages] = useState<string[]>([])
  const [uploadingImage, setUploadingImage] = useState(false)
  const [imageCapable, setImageCapable] = useState(false)
  const [showAttachmentMenu, setShowAttachmentMenu] = useState(false)
  const [redPacketComposerOpen, setRedPacketComposerOpen] = useState(false)
  const [redPacketAmountDraft, setRedPacketAmountDraft] = useState('')
  const [redPacketBlessingDraft, setRedPacketBlessingDraft] = useState('')
  const [redPacketComposerError, setRedPacketComposerError] = useState<string | null>(null)
  const [quoteDraft, setQuoteDraft] = useState<QuoteDraft | null>(null)
  const [messageMenu, setMessageMenu] = useState<MessageMenuState | null>(null)
  const [editState, setEditState] = useState<MessageEditState | null>(null)
  const [selectTextState, setSelectTextState] = useState<SelectTextState | null>(null)
  const [selectTextCopied, setSelectTextCopied] = useState(false)

  // Red Packet State
  const [activeRedPacket, setActiveRedPacket] = useState<ActiveRedPacketState | null>(null)
  const [packetStep, setPacketStep] = useState<'idle' | 'opening' | 'opened'>('idle')

  const containerRef = useRef<HTMLDivElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const selectTextTextareaRef = useRef<HTMLTextAreaElement>(null)
  const activeRequestRef = useRef<AbortController | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const attachmentButtonRef = useRef<HTMLButtonElement>(null)
  const attachmentPanelRef = useRef<HTMLDivElement>(null)
  const redPacketAmountInputRef = useRef<HTMLInputElement>(null)
  const streamingAssistantTimestampRef = useRef<string | null>(null)
  const revealingAssistantTimestampRef = useRef<string | null>(null)
  const assistantRevealReadySegmentsRef = useRef(0)
  const assistantRevealTimeoutRef = useRef<number | null>(null)
  const assistantRevealLastAtRef = useRef(0)
  const animatedBubbleKeysRef = useRef<Set<string>>(new Set())
  const bubbleKeysSeededRef = useRef(false)
  const keyboardOffsetRef = useRef(0)
  const keyboardOffsetRafRef = useRef<number | null>(null)
  const longPressTimeoutRef = useRef<number | null>(null)
  const longPressStartRef = useRef<{ x: number; y: number } | null>(null)
  const redPacketOpenTimeoutRef = useRef<number | null>(null)
  const lastPatAtRef = useRef(0)
  const pendingOutgoingMessagesRef = useRef<ChatMessage[]>([])
  const pendingOutgoingTimeoutRef = useRef<number | null>(null)
  const lastSessionIdRef = useRef(sessionId)
  const lastPromptIdRef = useRef<string | undefined>(promptId)

  useEffect(() => {
    if (!showAttachmentMenu) return
    const handlePointerDown = (event: MouseEvent | TouchEvent) => {
      const target = event.target as Node | null
      if (!target) return
      if (attachmentButtonRef.current && attachmentButtonRef.current.contains(target)) return
      if (attachmentPanelRef.current && attachmentPanelRef.current.contains(target)) return
      setShowAttachmentMenu(false)
    }
    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('touchstart', handlePointerDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('touchstart', handlePointerDown)
    }
  }, [showAttachmentMenu])

  useEffect(() => {
    if (!redPacketComposerOpen) return
    setRedPacketComposerError(null)
    window.setTimeout(() => {
      redPacketAmountInputRef.current?.focus()
    }, 0)
  }, [redPacketComposerOpen])

  useEffect(() => {
    const previousSessionId = lastSessionIdRef.current
    const previousPromptId = lastPromptIdRef.current
    if (previousSessionId !== sessionId) {
      clearPendingOutgoingTimeout()
      if (pendingOutgoingMessagesRef.current.length > 0) {
        void flushPendingOutgoingMessages('background', { sessionId: previousSessionId, promptId: previousPromptId })
      }
      pendingOutgoingMessagesRef.current = []
    }
    lastSessionIdRef.current = sessionId
    lastPromptIdRef.current = promptId

    activeRequestRef.current?.abort()
    activeRequestRef.current = null
    if (assistantRevealTimeoutRef.current !== null) {
      window.clearTimeout(assistantRevealTimeoutRef.current)
      assistantRevealTimeoutRef.current = null
    }
    if (longPressTimeoutRef.current !== null) {
      window.clearTimeout(longPressTimeoutRef.current)
      longPressTimeoutRef.current = null
    }
    if (redPacketOpenTimeoutRef.current !== null) {
      window.clearTimeout(redPacketOpenTimeoutRef.current)
      redPacketOpenTimeoutRef.current = null
    }
    streamingAssistantTimestampRef.current = null
    revealingAssistantTimestampRef.current = null
    assistantRevealReadySegmentsRef.current = 0
    assistantRevealLastAtRef.current = 0
    animatedBubbleKeysRef.current.clear()
    bubbleKeysSeededRef.current = false
    setSending(false)
    setAssistantResponseDone(false)
    setRevealingAssistantTimestamp(null)
    setAssistantVisibleSegments(0)
    setActiveRedPacket(null)
    setPacketStep('idle')
    setPendingImages([])
    setUploadingImage(false)
	    setQuoteDraft(null)
	    setMessageMenu(null)
	    setEditState(null)
	    setSelectTextState(null)
	    setSelectTextCopied(false)
      setShowAttachmentMenu(false)
      setRedPacketComposerOpen(false)
      setRedPacketAmountDraft('')
      setRedPacketBlessingDraft('')
      setRedPacketComposerError(null)
	    if (containerRef.current) {
	      gsap.set(containerRef.current, { x: '0%' })
	    }
	    loadSession()
	  }, [sessionId])

  useEffect(() => {
    lastPromptIdRef.current = promptId
  }, [promptId])

  useEffect(() => {
    return () => {
      clearPendingOutgoingTimeout()
      if (pendingOutgoingMessagesRef.current.length > 0) {
        void flushPendingOutgoingMessages('background', {
          sessionId: lastSessionIdRef.current,
          promptId: lastPromptIdRef.current,
        })
      }
      activeRequestRef.current?.abort()
      if (assistantRevealTimeoutRef.current !== null) {
        window.clearTimeout(assistantRevealTimeoutRef.current)
        assistantRevealTimeoutRef.current = null
      }
      if (longPressTimeoutRef.current !== null) {
        window.clearTimeout(longPressTimeoutRef.current)
        longPressTimeoutRef.current = null
      }
      if (redPacketOpenTimeoutRef.current !== null) {
        window.clearTimeout(redPacketOpenTimeoutRef.current)
        redPacketOpenTimeoutRef.current = null
      }
    }
  }, [])

	  useEffect(() => {
	    if (!selectTextState) return
	    setSelectTextCopied(false)
	    window.setTimeout(() => {
	      const textarea = selectTextTextareaRef.current
	      if (!textarea) return
	      textarea.focus()
	      textarea.select()
	    }, 0)
	  }, [selectTextState])

	  useEffect(() => {
	    const target = containerRef.current
	    if (!target) return

	    const applyOffset = (nextOffset: number) => {
	      const offset = Math.max(0, Math.round(nextOffset))
	      if (offset === keyboardOffsetRef.current) return
	      keyboardOffsetRef.current = offset
	      target.style.setProperty('--chat-keyboard-offset', `${offset}px`)
	      if (offset > 0) {
	        window.setTimeout(() => {
	          if (!messageListRef.current) return
	          messageListRef.current.scrollTop = messageListRef.current.scrollHeight
	        }, 0)
	      }
	    }

	    const update = () => {
	      if (keyboardOffsetRafRef.current !== null) return
	      keyboardOffsetRafRef.current = window.requestAnimationFrame(() => {
	        keyboardOffsetRafRef.current = null
	        const viewport = window.visualViewport
	        if (!viewport) {
	          applyOffset(0)
	          return
	        }
	        applyOffset(window.innerHeight - viewport.height - viewport.offsetTop)
	      })
	    }

	    const viewport = window.visualViewport
	    update()
	    window.addEventListener('resize', update)
	    viewport?.addEventListener('resize', update)
	    viewport?.addEventListener('scroll', update)

	    return () => {
	      window.removeEventListener('resize', update)
	      viewport?.removeEventListener('resize', update)
	      viewport?.removeEventListener('scroll', update)
	      if (keyboardOffsetRafRef.current !== null) {
	        window.cancelAnimationFrame(keyboardOffsetRafRef.current)
	        keyboardOffsetRafRef.current = null
	      }
	      keyboardOffsetRef.current = 0
	      target.style.setProperty('--chat-keyboard-offset', '0px')
	    }
	  }, [])

		  useEffect(() => {
		    if (loading) return
		    scrollToBottom()
		  }, [messages, loading])

	  useEffect(() => {
	    if (!sending) return
	    if (!revealingAssistantTimestamp) return

	    const currentAssistantMessage = messages.find(
	      (message) => message.role === 'assistant' && message.timestamp === revealingAssistantTimestamp
	    )
		    if (!currentAssistantMessage) return
	
		    const isStreamingAssistantMessage = streamingAssistantTimestampRef.current === revealingAssistantTimestamp
		    let assistantSegments = splitAssistantMessageContent(currentAssistantMessage.content)
		    const normalizedContent = normalizeAssistantContent(currentAssistantMessage.content)
		    const hasSplitToken = normalizedContent.includes(assistantMessageSplitToken)
		    const endsWithSplitToken = normalizedContent.trimEnd().endsWith(assistantMessageSplitToken)

	    const shouldHoldTrailingSegment =
	      isStreamingAssistantMessage && hasSplitToken && !endsWithSplitToken && assistantSegments.length > 1
	    if (shouldHoldTrailingSegment) {
	      assistantSegments = assistantSegments.slice(0, -1)
	    }

	    assistantRevealReadySegmentsRef.current = assistantSegments.length
	    if (assistantRevealTimeoutRef.current !== null) return
	    if (assistantVisibleSegments >= assistantRevealReadySegmentsRef.current) return

	    const now = performance.now()
	    const elapsed = now - assistantRevealLastAtRef.current
	    const delay = Math.max(0, assistantBubbleIntervalMs - elapsed)
	    assistantRevealTimeoutRef.current = window.setTimeout(() => {
	      assistantRevealTimeoutRef.current = null
	      assistantRevealLastAtRef.current = performance.now()
	      setAssistantVisibleSegments((prev) => Math.min(prev + 1, assistantRevealReadySegmentsRef.current))
	    }, delay)
	  }, [messages, revealingAssistantTimestamp, assistantVisibleSegments, sending])

	  useEffect(() => {
	    if (!sending) return
	    if (!assistantResponseDone) return
	    if (!revealingAssistantTimestamp) return

	    if (assistantVisibleSegments < assistantRevealReadySegmentsRef.current) return
	    if (assistantRevealTimeoutRef.current !== null) {
	      window.clearTimeout(assistantRevealTimeoutRef.current)
	      assistantRevealTimeoutRef.current = null
	    }
	    assistantRevealReadySegmentsRef.current = 0
	    revealingAssistantTimestampRef.current = null
	    setAssistantResponseDone(false)
	    setRevealingAssistantTimestamp(null)
	    setAssistantVisibleSegments(0)
	    setSending(false)
	  }, [assistantResponseDone, assistantVisibleSegments, revealingAssistantTimestamp, sending])

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

  type FlushMode = 'foreground' | 'background'

  const clearPendingOutgoingTimeout = () => {
    if (pendingOutgoingTimeoutRef.current !== null) {
      window.clearTimeout(pendingOutgoingTimeoutRef.current)
      pendingOutgoingTimeoutRef.current = null
    }
  }

  const getEffectivePromptId = () => promptId || prompt?.id || session?.prompt_id

  const buildSendPayloadMessages = (outgoingMessages: ChatMessage[]) =>
    outgoingMessages.map(({ role, content, image_paths, tool_calls }) => ({
      role,
      content,
      ...(image_paths ? { image_paths } : {}),
      ...(tool_calls ? { tool_calls } : {}),
    }))

  const flushPendingOutgoingMessages = async (mode: FlushMode, override?: { sessionId: string; promptId?: string }) => {
    if (mode === 'foreground' && sending) return

    const pendingMessages = pendingOutgoingMessagesRef.current
    if (pendingMessages.length === 0) return

    pendingOutgoingMessagesRef.current = []
    clearPendingOutgoingTimeout()

    const targetSessionId = override?.sessionId || sessionId
    const targetPromptId = override?.promptId || getEffectivePromptId()

    if (mode === 'background') {
      try {
        await sendMessage(targetSessionId, buildSendPayloadMessages(pendingMessages), {
          promptId: targetPromptId,
          stream: false,
        })
      } catch {
        // ignore background errors
      }
      return
    }

    if (assistantRevealTimeoutRef.current !== null) {
      window.clearTimeout(assistantRevealTimeoutRef.current)
      assistantRevealTimeoutRef.current = null
    }
    assistantRevealReadySegmentsRef.current = 0
    assistantRevealLastAtRef.current = performance.now()
    revealingAssistantTimestampRef.current = null
    streamingAssistantTimestampRef.current = null
    setAssistantResponseDone(false)
    setRevealingAssistantTimestamp(null)
    setAssistantVisibleSegments(0)

    setSending(true)

    let aborted = false
    try {
      activeRequestRef.current?.abort()
      const abortController = new AbortController()
      activeRequestRef.current = abortController

      const response = await sendMessage(targetSessionId, buildSendPayloadMessages(pendingMessages), {
        promptId: targetPromptId,
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
        streamingAssistantTimestampRef.current = assistantTimestamp
        revealingAssistantTimestampRef.current = assistantTimestamp
        setRevealingAssistantTimestamp(assistantTimestamp)
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
        streamingAssistantTimestampRef.current = null
        setAssistantResponseDone(true)
      } else {
        // 非流式响应处理
        const result = await response.json()

        if (result.success && result.data?.response?.choices?.[0]?.message) {
          const msg = result.data.response.choices[0].message
          const assistantTimestamp = new Date().toISOString()
          revealingAssistantTimestampRef.current = assistantTimestamp
          setRevealingAssistantTimestamp(assistantTimestamp)
          const assistantMessage: ChatMessage = {
            role: 'assistant',
            content: msg.content || '',
            reasoning_content: msg.reasoning_content,
            tool_calls: msg.tool_calls,
            timestamp: assistantTimestamp
          }
          setMessages(prev => [...prev, assistantMessage])
          setAssistantResponseDone(true)
        } else if (result.success === false && result.error) {
          throw new Error(result.error)
        } else {
          throw new Error('Invalid response format')
        }
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        aborted = true
        return
      }
      if (error instanceof Error && (error as Error & { name?: string }).name === 'AbortError') {
        aborted = true
        return
      }

      const message = error instanceof Error && error.message ? error.message : '发送失败，请重试'
      const fallbackAssistantTimestamp = new Date().toISOString()
      const streamingTimestamp = streamingAssistantTimestampRef.current
      if (!streamingTimestamp) {
        revealingAssistantTimestampRef.current = fallbackAssistantTimestamp
        setRevealingAssistantTimestamp(fallbackAssistantTimestamp)
      }
      setMessages(prev => {
        if (streamingTimestamp) {
          const next = [...prev]
          for (let i = next.length - 1; i >= 0; i--) {
            const msg = next[i]
            if (msg.role === 'assistant' && msg.timestamp === streamingTimestamp) {
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
            timestamp: fallbackAssistantTimestamp,
          },
        ]
      })
      streamingAssistantTimestampRef.current = null
      setAssistantResponseDone(true)
    } finally {
      activeRequestRef.current = null
      if (aborted) {
        streamingAssistantTimestampRef.current = null
        revealingAssistantTimestampRef.current = null
        assistantRevealReadySegmentsRef.current = 0
        setAssistantResponseDone(false)
        setRevealingAssistantTimestamp(null)
        setAssistantVisibleSegments(0)
        setSending(false)
      }
    }
  }

  const schedulePendingOutgoingFlush = () => {
    const config = getReplyWaitWindowConfig()
    const delayMs = Math.max(0, Math.round(config.seconds * 1000))
    if (delayMs <= 0) {
      void flushPendingOutgoingMessages('foreground')
      return
    }

    if (config.mode === 'fixed') {
      if (pendingOutgoingTimeoutRef.current !== null) return
      pendingOutgoingTimeoutRef.current = window.setTimeout(() => {
        pendingOutgoingTimeoutRef.current = null
        void flushPendingOutgoingMessages('foreground')
      }, delayMs)
      return
    }

    clearPendingOutgoingTimeout()
    pendingOutgoingTimeoutRef.current = window.setTimeout(() => {
      pendingOutgoingTimeoutRef.current = null
      void flushPendingOutgoingMessages('foreground')
    }, delayMs)
  }

  const handleBack = () => {
    if (!sending && pendingOutgoingMessagesRef.current.length > 0) {
      void flushPendingOutgoingMessages('background')
    }

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

  type SendOutgoingOptions = {
    clearChatInput?: boolean
    clearQuoteDraft?: boolean
    clearPendingImages?: boolean
    resetTextareaHeight?: boolean
  }

  const sendOutgoingMessage = async (userMessage: ChatMessage, options: SendOutgoingOptions = {}) => {
    if (sending || uploadingImage) return

    setMessages(prev => [...prev, userMessage])

    if (options.clearChatInput) {
      setInputText('')
    }
    if (options.clearQuoteDraft) {
      setQuoteDraft(null)
    }
    if (options.clearPendingImages) {
      setPendingImages([])
    }

    if (options.resetTextareaHeight && textareaRef.current) {
      textareaRef.current.style.height = '36px'
    }
    pendingOutgoingMessagesRef.current.push(userMessage)
    schedulePendingOutgoingFlush()
  }

  const handleSend = async () => {
    const trimmedText = inputText.trim()
    const hasImages = pendingImages.length > 0
    if ((!trimmedText && !hasImages) || sending || uploadingImage) return
    if (hasImages && !imageCapable) {
      alert('当前模型不支持图片输入')
      return
    }

    const finalText = quoteDraft ? buildQuotedOutgoingContent(quoteDraft.line, trimmedText) : trimmedText
    const userMessage: ChatMessage = {
      role: 'user',
      content: finalText,
      timestamp: new Date().toISOString(),
      ...(hasImages ? { image_paths: pendingImages } : {}),
    }

    await sendOutgoingMessage(userMessage, {
      clearChatInput: true,
      clearQuoteDraft: true,
      clearPendingImages: true,
      resetTextareaHeight: true,
    })
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

  const handleAttachmentButtonClick = () => {
    setShowAttachmentMenu(prev => !prev)
  }

  const openRedPacketComposer = () => {
    if (sending) return
    setShowAttachmentMenu(false)
    setRedPacketAmountDraft('')
    setRedPacketBlessingDraft('')
    setRedPacketComposerError(null)
    setRedPacketComposerOpen(true)
  }

  const closeRedPacketComposer = () => {
    setRedPacketComposerOpen(false)
    setRedPacketComposerError(null)
  }

  const handleRedPacketComposerSend = () => {
    setRedPacketComposerError(null)
    const amountValue = Number.parseFloat(redPacketAmountDraft)
    if (!Number.isFinite(amountValue) || amountValue <= 0) {
      setRedPacketComposerError('请输入正确的金额')
      return
    }
    const blessing = redPacketBlessingDraft.trim()
    if (!blessing) {
      setRedPacketComposerError('请输入祝福语')
      return
    }
    if (blessing.length > 10) {
      setRedPacketComposerError('祝福语不能超过10个字')
      return
    }

    const normalizedAmount = Math.round(amountValue * 100) / 100
    const rawId =
      typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
        ? crypto.randomUUID()
        : `${Date.now().toString(16)}${Math.random().toString(16).slice(2)}`
    const toolCallId = `rp_${rawId.replace(/[^a-zA-Z0-9]/g, '')}`

    const toolCall: ToolCall = {
      id: toolCallId,
      type: 'function',
      function: {
        name: 'send_red_packet',
        arguments: JSON.stringify({ amount: normalizedAmount, message: blessing }),
      },
    }

    const userMessage: ChatMessage = {
      role: 'user',
      content: '',
      timestamp: new Date().toISOString(),
      tool_calls: [toolCall],
    }

    setRedPacketComposerOpen(false)
    setRedPacketAmountDraft('')
    setRedPacketBlessingDraft('')
    void sendOutgoingMessage(userMessage)
  }

  const handlePatAssistant = () => {
    if (sending || uploadingImage) return
    const now = Date.now()
    if (now - lastPatAtRef.current < 800) return
    lastPatAtRef.current = now

    const rawId =
      typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
        ? crypto.randomUUID()
        : `${Date.now().toString(16)}${Math.random().toString(16).slice(2)}`
    const toolCallId = `pat_${rawId.replace(/[^a-zA-Z0-9]/g, '')}`

    const toolCall: ToolCall = {
      id: toolCallId,
      type: 'function',
      function: {
        name: 'send_pat',
        arguments: JSON.stringify({ name: userInfo?.username?.trim() || '我', target: '你' }),
      },
    }

    const userMessage: ChatMessage = {
      role: 'user',
      content: '',
      timestamp: new Date().toISOString(),
      tool_calls: [toolCall],
    }

    void sendOutgoingMessage(userMessage)
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

  const getRoleDisplayName = (role: string) => {
    if (role === 'assistant') return prompt?.name || 'AI'
    return userInfo?.username || '我'
  }

	  const buildQuoteLineFromMessage = (message: ChatMessage) => {
	    const name = getRoleDisplayName(message.role)
	    const rawContent = message.role === 'assistant' ? normalizeAssistantContent(message.content) : message.content
	    const parsed = parseQuotedMessageContent(rawContent)
	    let quoteText = (parsed ? parsed.text : rawContent).trim()
	    if (!quoteText && message.image_paths && message.image_paths.length > 0) {
	      quoteText = '图片'
	    }
	    quoteText = quoteText.replace(/\s+/g, ' ').trim()
    const maxLen = 80
    if (quoteText.length > maxLen) {
      quoteText = quoteText.slice(0, maxLen) + '...'
	  }
    return `${name}：${quoteText || '...'}`
  }

  const cancelLongPress = () => {
    if (longPressTimeoutRef.current !== null) {
      window.clearTimeout(longPressTimeoutRef.current)
      longPressTimeoutRef.current = null
    }
    longPressStartRef.current = null
  }

  const openMessageMenuAt = (position: { x: number; y: number }, messageIndex: number, message: ChatMessage) => {
    cancelLongPress()
    setMessageMenu({ position, messageIndex, message })
  }

  const handleMessageContextMenu = (e: React.MouseEvent, item: DisplayItem) => {
    if (item.type !== 'text') return
    e.preventDefault()
    openMessageMenuAt({ x: e.clientX, y: e.clientY }, item.messageIndex, item.message)
  }

  const handleMessagePointerDown = (e: React.PointerEvent, item: DisplayItem) => {
    if (item.type !== 'text') return
    if (e.pointerType === 'mouse' && e.button !== 0) return
    cancelLongPress()

    const position = { x: e.clientX, y: e.clientY }
    longPressStartRef.current = position
    longPressTimeoutRef.current = window.setTimeout(() => {
      longPressTimeoutRef.current = null
      openMessageMenuAt(position, item.messageIndex, item.message)
    }, 500)
  }

  const handleMessagePointerMove = (e: React.PointerEvent) => {
    if (longPressTimeoutRef.current === null || !longPressStartRef.current) return
    const dx = e.clientX - longPressStartRef.current.x
    const dy = e.clientY - longPressStartRef.current.y
    if (Math.hypot(dx, dy) > 10) {
      cancelLongPress()
    }
  }

  const handleMessagePointerUp = () => {
    cancelLongPress()
  }

  const handleMessagePointerCancel = () => {
    cancelLongPress()
  }

  const handleMessagePointerLeave = () => {
    cancelLongPress()
  }

  const handleStartEditMessage = (messageIndex: number) => {
    const message = messages[messageIndex]
    if (!message) return
    const parsed = parseQuotedMessageContent(message.content)
    const quoteLine = parsed?.quoteLine || null
    const text = parsed ? parsed.text : message.content
    setEditState({ messageIndex, quoteLine, text })
    setMessageMenu(null)
  }

  const handleSaveEditMessage = async () => {
    if (!editState) return
    const content = editState.quoteLine ? buildQuotedOutgoingContent(editState.quoteLine, editState.text) : editState.text
    const updated = await updateSessionMessage(sessionId, editState.messageIndex, content)
    if (!updated) {
      alert('编辑失败，请重试')
      return
    }
    setSession(updated)
    setMessages(updated.messages || [])
    setEditState(null)
  }

  const handleDeleteMessage = async (messageIndex: number) => {
    const ok = window.confirm('确定删除这条消息吗？')
    if (!ok) return

    const updated = await deleteSessionMessage(sessionId, messageIndex)
    if (!updated) {
      alert('删除失败，请重试')
      return
    }
    setSession(updated)
    setMessages(updated.messages || [])
    setMessageMenu(null)
  }

  const handleQuoteMessage = (message: ChatMessage) => {
    setQuoteDraft({ line: buildQuoteLineFromMessage(message) })
    setMessageMenu(null)
    textareaRef.current?.focus()
  }

	  const buildSelectableText = (message: ChatMessage) => {
	    const rawContent = message.role === 'assistant' ? normalizeAssistantContent(message.content) : message.content
	    const parsed = parseQuotedMessageContent(rawContent)
	    if (!parsed) return rawContent
	    const quoteLine = parsed.quoteLine.trim()
	    const text = parsed.text
	    if (quoteLine && text.trim() !== '') {
	      return `${quoteLine}\n${text}`
	    }
	    return quoteLine || text
	  }

  const handleCopySelectText = async () => {
    if (!selectTextState) return
    const text = selectTextState.text
    let copied = false

    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text)
        copied = true
      } catch {
        copied = false
      }
    }

    if (!copied) {
      const textarea = selectTextTextareaRef.current
      if (textarea) {
        textarea.focus()
        textarea.select()
        try {
          copied = document.execCommand('copy')
        } catch {
          copied = false
        }
      }
    }

    if (!copied) {
      alert('复制失败，请手动选择文本复制')
      return
    }

    setSelectTextCopied(true)
    window.setTimeout(() => setSelectTextCopied(false), 1500)
  }

  const handleRecallMessage = async (messageIndex: number) => {
    const updated = await recallSessionMessage(sessionId, messageIndex)
    if (!updated) {
      alert('撤回失败，请重试')
      return
    }
    setSession(updated)
    setMessages(updated.messages || [])
    setMessageMenu(null)
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
        <div
          className="message-avatar"
          onDoubleClick={(e) => {
            e.stopPropagation()
            handlePatAssistant()
          }}
          title="双击拍一拍"
        >
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

  const openedRedPacketKeys = useMemo(() => {
    const keys = new Set<string>()
    messages.forEach((message) => {
      const toolCalls = message.tool_calls || []
      toolCalls.forEach((toolCall) => {
        if (toolCall.function.name !== 'red_packet_received') return
        try {
          const args = JSON.parse(toolCall.function.arguments || '{}') as { packet_key?: unknown }
          const packetKey = typeof args.packet_key === 'string' ? args.packet_key.trim() : ''
          if (packetKey) keys.add(packetKey)
        } catch {
          // ignore invalid payload
        }
      })
    })
    return keys
  }, [messages])

  const normalizePacketKey = (rawKey: string) => rawKey.replace(/[^a-zA-Z0-9_-]/g, '_')
  const derivePacketKeys = (toolCall: ToolCall, rawKey: string) => {
    const legacyKey = normalizePacketKey(rawKey)
    const primaryKey =
      typeof toolCall.id === 'string' && toolCall.id.trim() !== '' ? normalizePacketKey(toolCall.id.trim()) : legacyKey
    return { primaryKey, legacyKey }
  }

  const getRedPacketReceivedRecord = (packetKey: string) => {
    const normalizedTarget = normalizePacketKey(packetKey)
    for (const message of messages) {
      const toolCalls = message.tool_calls || []
      for (const toolCall of toolCalls) {
        if (toolCall.function.name !== 'red_packet_received') continue
        try {
          const args = JSON.parse(toolCall.function.arguments || '{}') as {
            packet_key?: unknown
            receiver_name?: unknown
            sender_name?: unknown
          }
          const rawKey = typeof args.packet_key === 'string' ? args.packet_key.trim() : ''
          if (!rawKey) continue
          if (normalizePacketKey(rawKey) !== normalizedTarget) continue

          const receiverName =
            typeof args.receiver_name === 'string' && args.receiver_name.trim() !== '' ? args.receiver_name.trim() : ''
          const senderName =
            typeof args.sender_name === 'string' && args.sender_name.trim() !== '' ? args.sender_name.trim() : ''

          return { receiverName, senderName, timestamp: message.timestamp }
        } catch {
          // ignore invalid payload
        }
      }
    }
    return null as null | { receiverName: string; senderName: string; timestamp: string }
  }

  const formatRedPacketTime = (timestamp: string) => {
    const date = new Date(timestamp)
    if (!Number.isFinite(date.getTime())) return ''
    const month = date.getMonth() + 1
    const day = date.getDate()
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')
    return `${month}月${day}日 ${hours}:${minutes}`
  }

  const inferRedPacketParties = (
    packetKey: string
  ): null | { senderRole: 'user' | 'assistant'; senderName: string; receiverName: string } => {
    const normalizedTarget = normalizePacketKey(packetKey)
    for (const message of messages) {
      const toolCalls = message.tool_calls || []
      for (let toolIndex = 0; toolIndex < toolCalls.length; toolIndex++) {
        const toolCall = toolCalls[toolIndex]
        if (toolCall.function.name !== 'send_red_packet') continue
        const rawKey = `${message.timestamp}-rp-${toolCall.id || toolIndex}`
        const { primaryKey, legacyKey } = derivePacketKeys(toolCall, rawKey)
        if (primaryKey !== normalizedTarget && legacyKey !== normalizedTarget) continue

        const senderRole: 'user' | 'assistant' = message.role === 'user' ? 'user' : 'assistant'
        const senderName = senderRole === 'user' ? userInfo?.username?.trim() || '你' : prompt?.name?.trim() || 'AI Assistant'
        const receiverName =
          senderRole === 'user' ? prompt?.name?.trim() || 'AI Assistant' : userInfo?.username?.trim() || '你'
        return { senderRole, senderName, receiverName }
      }
    }
    return null
  }

  // 渲染红包卡片
  const renderRedPacket = (toolCall: ToolCall, rawKey: string, role: 'user' | 'assistant') => {
    if (toolCall.function.name !== 'send_red_packet') return null

    try {
      const params: RedPacketParams = JSON.parse(toolCall.function.arguments)
      const { primaryKey, legacyKey } = derivePacketKeys(toolCall, rawKey)
      const opened = openedRedPacketKeys.has(primaryKey) || openedRedPacketKeys.has(legacyKey)
      const shouldTreatAsOpened = role === 'user' || opened
      const packetKey = openedRedPacketKeys.has(legacyKey) ? legacyKey : primaryKey
      const senderName = role === 'user' ? userInfo?.username?.trim() || '我' : prompt?.name?.trim() || 'AI Assistant'
      const senderAvatarSrc = role === 'user' ? getUserAvatarSrc() : getPromptAvatarSrc()
	      return (
	        <div 
	          className="red-packet-bubble"
	          data-bubble-key={rawKey}
	          onClick={() => {
	            setActiveRedPacket({ params, packetKey, senderRole: role, senderName, senderAvatarSrc })
	            setPacketStep(shouldTreatAsOpened ? 'opened' : 'idle')
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
              <div className="rp-status">{role === 'user' ? '查看红包' : shouldTreatAsOpened ? '已领取' : '领取红包'}</div>
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

  const renderRedPacketReceivedBanner = (toolCall: ToolCall) => {
    if (toolCall.function.name !== 'red_packet_received') return null

    let receiverName = userInfo?.username?.trim() || '你'
    let senderName = prompt?.name?.trim() || 'AI Assistant'
    let packetKey = ''
    let inferredSenderRole: null | 'user' | 'assistant' = null

    try {
      const args = JSON.parse(toolCall.function.arguments || '{}') as {
        packet_key?: unknown
        receiver_name?: unknown
        sender_name?: unknown
      }
      if (typeof args.packet_key === 'string' && args.packet_key.trim() !== '') {
        packetKey = args.packet_key.trim()
      }

      if (packetKey) {
        const inferred = inferRedPacketParties(packetKey)
        if (inferred) {
          receiverName = inferred.receiverName
          senderName = inferred.senderName
          inferredSenderRole = inferred.senderRole
        }
      }

      const shouldTrustToolNames = inferredSenderRole !== 'user'
      if (shouldTrustToolNames) {
        if (typeof args.receiver_name === 'string' && args.receiver_name.trim() !== '') {
          receiverName = args.receiver_name.trim()
        }
        if (typeof args.sender_name === 'string' && args.sender_name.trim() !== '') {
          senderName = args.sender_name.trim()
        }
      }
    } catch {
      // ignore invalid payload
    }

    return (
      <div className="red-packet-received-banner">
        <svg className="red-packet-received-banner-icon" viewBox="0 0 24 24" aria-hidden="true">
          <rect x="3" y="3" width="18" height="18" rx="4" fill="#e8554e" />
          <circle cx="12" cy="12" r="3.6" fill="#f5d27a" />
          <circle cx="12" cy="12" r="1.4" fill="#d4a94a" />
        </svg>
        <span className="red-packet-received-banner-text">
          {receiverName}领取了{senderName}的<span className="red-packet-received-banner-highlight">红包</span>
        </span>
      </div>
    )
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

  const closeRedPacketModal = () => {
    if (redPacketOpenTimeoutRef.current !== null) {
      window.clearTimeout(redPacketOpenTimeoutRef.current)
      redPacketOpenTimeoutRef.current = null
    }
    setActiveRedPacket(null)
  }

  const handleOpenPacket = () => {
    if (!activeRedPacket) return
    if (packetStep !== 'idle') return

    const packetKey = activeRedPacket.packetKey
    const receiverName = userInfo?.username?.trim() || '你'
    const senderName = prompt?.name?.trim() || 'AI Assistant'

    setPacketStep('opening')
    if (redPacketOpenTimeoutRef.current !== null) {
      window.clearTimeout(redPacketOpenTimeoutRef.current)
    }
    redPacketOpenTimeoutRef.current = window.setTimeout(() => {
      redPacketOpenTimeoutRef.current = null
      setPacketStep('opened')
      void (async () => {
        const updated = await openSessionRedPacket(sessionId, packetKey, receiverName, senderName)
        if (!updated) return
        setSession(updated)
        setMessages(updated.messages || [])
      })()
    }, 1000)
  }

  const buildDisplayItems = (): DisplayItem[] => {
    const items: DisplayItem[] = []
    messages.forEach((message, index) => {
      const hasImages = !!(message.image_paths && message.image_paths.length > 0)
      const toolCalls = message.tool_calls || []
      const supportedCalls = toolCalls.filter(tc => tc.function.name === 'send_red_packet' || tc.function.name === 'send_pat' || tc.function.name === 'red_packet_received')

      const isAssistant = message.role === 'assistant'
	      const isStreamingAssistantMessage =
	        isAssistant &&
	        sending &&
	        streamingAssistantTimestampRef.current !== null &&
	        message.timestamp === streamingAssistantTimestampRef.current
	
	      let assistantSegments = isAssistant ? splitAssistantMessageContent(message.content) : []
	      const normalizedContent = isAssistant ? normalizeAssistantContent(message.content) : ''
	      const hasSplitToken = isAssistant && normalizedContent.includes(assistantMessageSplitToken)
	      const endsWithSplitToken = isAssistant && normalizedContent.trimEnd().endsWith(assistantMessageSplitToken)

      const shouldHoldTrailingSegment =
        isStreamingAssistantMessage && hasSplitToken && !endsWithSplitToken && assistantSegments.length > 1

      if (shouldHoldTrailingSegment) {
        assistantSegments = assistantSegments.slice(0, -1)
      }

      const isRevealingAssistantMessage =
        isAssistant && revealingAssistantTimestamp !== null && message.timestamp === revealingAssistantTimestamp
      if (isRevealingAssistantMessage) {
        assistantSegments = assistantSegments.slice(0, assistantVisibleSegments)
      }
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
                messageIndex: index,
              })
            })

          } else {
            items.push({
              key: `${message.timestamp}-text-0`,
              role: message.role,
              type: 'text',
              message: { ...message, content: '' },
              messageIndex: index,
            })
          }
	        } else {
	          if (isRecalledMessage(message)) {
	            items.push({
	              key: `${message.timestamp}-recalled`,
	              role: message.role,
	              type: 'recall-banner',
	              message,
	              messageIndex: index,
	            })
	          } else {
	            items.push({
	              key: `${message.timestamp}-text`,
	              role: message.role,
	              type: 'text',
	              message,
	              messageIndex: index,
	            })
	          }
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
            messageIndex: index,
          })
        }
        if (toolCall.function.name === 'send_pat') {
          items.push({
            key: `${message.timestamp}-pat-${toolCall.id || toolIndex}`,
            role: message.role,
            type: 'pat-banner',
            message,
            toolCall,
            messageIndex: index,
          })
        }
        if (toolCall.function.name === 'red_packet_received') {
          items.push({
            key: `${message.timestamp}-rp-received-${toolCall.id || toolIndex}`,
            role: message.role,
            type: 'red-packet-received-banner',
            message,
            toolCall,
            messageIndex: index,
          })
        }
      })

    })
    return items
  }

  const renderMessageBubbleContent = (item: DisplayItem) => {
    const parsedQuote = parseQuotedMessageContent(item.message.content)
    const quoteLine = parsedQuote?.quoteLine || ''
    const text = parsedQuote ? parsedQuote.text : item.message.content
    const images = renderMessageImages(item.message)
    const hasText = text && text.trim() !== ''
    return (
      <div className="message-content">
        {quoteLine && <div className="message-quote">{quoteLine}</div>}
        {images}
        {hasText && <div className="message-text">{text}</div>}
      </div>
    )
  }

  const displayItems = buildDisplayItems()
  const canSend = (inputText.trim() !== '' || pendingImages.length > 0) && !sending && !uploadingImage

  useEffect(() => {
    if (loading) return
    if (!messageListRef.current) return

    if (!bubbleKeysSeededRef.current) {
      displayItems.forEach((item) => animatedBubbleKeysRef.current.add(item.key))
      bubbleKeysSeededRef.current = true
      return
    }

    const escapeForSelector = (value: string) => {
      if (typeof window !== 'undefined' && window.CSS && typeof window.CSS.escape === 'function') {
        return window.CSS.escape(value)
      }
      return value.replace(/["\\]/g, '\\$&')
    }

    displayItems.forEach((item) => {
      if (animatedBubbleKeysRef.current.has(item.key)) return
      animatedBubbleKeysRef.current.add(item.key)

      if (item.type !== 'text' && item.type !== 'red-packet') return
      const target = messageListRef.current?.querySelector<HTMLElement>(
        `[data-bubble-key="${escapeForSelector(item.key)}"]`
      )
      if (!target) return

      gsap.killTweensOf(target)
      gsap.fromTo(
        target,
        { opacity: 0, y: 12, scale: 0.97 },
        { opacity: 1, y: 0, scale: 1, duration: 0.32, ease: 'power2.out', clearProps: 'transform,opacity' }
      )
    })
  }, [displayItems, loading])

  return (
    <div className="chat-detail" ref={containerRef}>
      <div className="chat-detail-header">
        <button className="back-button" onClick={handleBack}>
          <svg viewBox="0 0 24 24">
            <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
          </svg>
	        </button>
	        <div className={`chat-detail-title${sending && assistantVisibleSegments === 0 ? ' typing' : ''}`}>
	          {sending && assistantVisibleSegments === 0 ? '对方正在输入中…' : (session?.title || '对话')}
	        </div>
	        {prompt && (
	          <button className="settings-button" onClick={() => setShowSettings(true)}>
	            <svg viewBox="0 0 24 24">
	              <path d="M6 10c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm6 0c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm6 0c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2z" />
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
	            const isRedPacketReceivedBanner = item.type === 'red-packet-received-banner'
	            const isPatBanner = item.type === 'pat-banner'
	            const isRecallBanner = item.type === 'recall-banner'
	            if (isRedPacketReceivedBanner) {
	              const banner = renderRedPacketReceivedBanner(item.toolCall)
	              if (!banner) return null
	              return (
	                <div key={item.key} className="message-item pat-banner-item">
	                  {banner}
	                </div>
	              )
	            }
	            if (isPatBanner) {
	              const banner = renderPatBanner(item.toolCall)
	              if (!banner) return null
	              return (
	                <div key={item.key} className="message-item pat-banner-item">
	                  {banner}
	                </div>
	              )
	            }
	
	            if (isRecallBanner) {
	              return (
	                <div key={item.key} className="message-item pat-banner-item">
	                  <div className="pat-banner">你撤回了一条消息</div>
	                </div>
	              )
	            }
	
	            const content = isRedPacket ? (
	              renderRedPacket(item.toolCall, item.key, item.role === 'user' ? 'user' : 'assistant')
	            ) : (
	              <div
	                className="message-bubble"
	                data-bubble-key={item.key}
	                onContextMenu={(e) => handleMessageContextMenu(e, item)}
	                onCopy={(e) => e.preventDefault()}
	                onCut={(e) => e.preventDefault()}
	                onPointerDown={(e) => handleMessagePointerDown(e, item)}
                onPointerMove={handleMessagePointerMove}
                onPointerUp={handleMessagePointerUp}
                onPointerCancel={handleMessagePointerCancel}
                onPointerLeave={handleMessagePointerLeave}
              >
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
        {quoteDraft && (
          <div className="chat-quote-preview">
            <div className="chat-quote-preview-text">{quoteDraft.line}</div>
            <button
              type="button"
              className="chat-quote-preview-close"
              onClick={() => setQuoteDraft(null)}
              aria-label="关闭引用"
            >
              ×
            </button>
          </div>
        )}

        {showAttachmentMenu && (
          <div className="attachment-expand-panel" ref={attachmentPanelRef} role="menu">
            <div className="attachment-grid">
              <button
                type="button"
                className="attachment-tile"
                onClick={() => {
                  setShowAttachmentMenu(false)
                  handleUploadClick()
                }}
                disabled={!imageCapable || uploadingImage}
                aria-label="相册"
                role="menuitem"
              >
                <div className="attachment-tile-icon">
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M19 7h-3V5c0-1.1-.9-2-2-2h-4c-1.1 0-2 .9-2 2v2H5c-1.1 0-2 .9-2 2v9c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V9c0-1.1-.9-2-2-2zm-9 0V5h4v2h-4zm7 13H5V9h14v11zM7 18l3-4 2 3 3-4 2 5H7z" />
                  </svg>
                </div>
                <div className="attachment-tile-label">相册</div>
              </button>

              <button
                type="button"
                className="attachment-tile"
                onClick={openRedPacketComposer}
                disabled={sending}
                aria-label="红包"
                role="menuitem"
              >
                <div className="attachment-tile-icon">
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M7 3h10a2 2 0 0 1 2 2v4a3 3 0 0 1-3 3H8a3 3 0 0 1-3-3V5a2 2 0 0 1 2-2zm0 12h10a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-2a2 2 0 0 1 2-2zm5-10a1.2 1.2 0 1 0 0 2.4A1.2 1.2 0 0 0 12 5zm0 12a1.2 1.2 0 1 0 0 2.4A1.2 1.2 0 0 0 12 17z" />
                  </svg>
                </div>
                <div className="attachment-tile-label">红包</div>
              </button>
            </div>
          </div>
        )}

        <div className="chat-input-row">
          <button
            ref={attachmentButtonRef}
            type="button"
            className={`attachment-button ${showAttachmentMenu ? 'open' : ''}`}
            onClick={handleAttachmentButtonClick}
            aria-label="更多功能"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M19 11H13V5a1 1 0 0 0-2 0v6H5a1 1 0 0 0 0 2h6v6a1 1 0 0 0 2 0v-6h6a1 1 0 0 0 0-2z" />
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
	            onFocus={() => window.setTimeout(scrollToBottom, 0)}
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
      </div>

	      {messageMenu && (
	        <ContextMenu
	          items={(() => {
	            const items: MenuItem[] = []
	            const selectableText = buildSelectableText(messageMenu.message).trim()
	            if (selectableText) {
	              items.push({ label: '选择文本', onClick: () => setSelectTextState({ text: selectableText }) })
	            }
	
	            if (!sending && messageMenu.message.role === 'user' && !isRecalledMessage(messageMenu.message)) {
	              items.push({ label: '撤回', onClick: () => handleRecallMessage(messageMenu.messageIndex) })
	            }
	
	            if (!sending) {
	              items.push({ label: '编辑', onClick: () => handleStartEditMessage(messageMenu.messageIndex) })
	              items.push({ label: '删除', danger: true, onClick: () => handleDeleteMessage(messageMenu.messageIndex) })
	            }
	
	            items.push({ label: '引用', onClick: () => handleQuoteMessage(messageMenu.message) })
	            return items
	          })()}
	          position={messageMenu.position}
	          onClose={() => setMessageMenu(null)}
	        />
	      )}

	      {selectTextState &&
	        createPortal(
	          <div className="select-text-overlay" onClick={() => setSelectTextState(null)}>
	            <div className="select-text-card" onClick={(e) => e.stopPropagation()}>
	              <div className="select-text-header">
	                <div className="select-text-title">选择文本</div>
	                <button
	                  type="button"
	                  className="select-text-close"
	                  onClick={() => setSelectTextState(null)}
	                  aria-label="关闭选择文本"
	                >
	                  ×
	                </button>
	              </div>

	              <textarea
	                ref={selectTextTextareaRef}
	                className="select-text-textarea"
	                value={selectTextState.text}
	                readOnly
	                rows={6}
	              />

	              <div className="select-text-footer">
	                {selectTextCopied && <div className="select-text-hint">已复制</div>}
	                <button type="button" className="select-text-btn copy" onClick={handleCopySelectText}>
	                  复制
	                </button>
	                <button type="button" className="select-text-btn" onClick={() => setSelectTextState(null)}>
	                  关闭
	                </button>
	              </div>
	            </div>
	          </div>,
	          document.body
	        )}
	
	      {editState &&
	        createPortal(
	          <div className="message-edit-overlay" onClick={() => setEditState(null)}>
            <div className="message-edit-card" onClick={(e) => e.stopPropagation()}>
              <div className="message-edit-header">
                <div className="message-edit-title">编辑消息</div>
                <button
                  type="button"
                  className="message-edit-close"
                  onClick={() => setEditState(null)}
                  aria-label="关闭编辑"
                >
                  ×
                </button>
              </div>

              {editState.quoteLine && <div className="message-edit-quote">{editState.quoteLine}</div>}

              <textarea
                className="message-edit-input"
                value={editState.text}
                onChange={(e) => setEditState(prev => (prev ? { ...prev, text: e.target.value } : prev))}
                rows={6}
              />

              <div className="message-edit-footer">
                <button type="button" className="message-edit-btn cancel" onClick={() => setEditState(null)}>
                  取消
                </button>
                <button
                  type="button"
                  className="message-edit-btn save"
                  onClick={handleSaveEditMessage}
                  disabled={!editState.quoteLine && editState.text.trim() === ''}
                >
                  保存
                </button>
              </div>
            </div>
          </div>,
          document.body
        )}

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
        (() => {
          const received = getRedPacketReceivedRecord(activeRedPacket.packetKey)
          const senderName = activeRedPacket.senderName
          const senderMessage = activeRedPacket.params.message || '恭喜发财，大吉大利'

          if (packetStep === 'opened' && activeRedPacket.senderRole === 'user') {
            const receiverName = prompt?.name?.trim() || 'AI Assistant'
            const receiverTime = received?.timestamp ? formatRedPacketTime(received.timestamp) : ''
            const receiverAvatarSrc = getPromptAvatarSrc()
            return (
              <div className="rp-detail-overlay">
                <div className="rp-detail-top">
                  <div className="rp-detail-nav">
                    <button type="button" className="rp-detail-back" onClick={closeRedPacketModal} aria-label="返回">
                      <svg viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M15.5 5.5a1 1 0 0 1 0 1.4L10.4 12l5.1 5.1a1 1 0 1 1-1.4 1.4l-5.8-5.8a1 1 0 0 1 0-1.4l5.8-5.8a1 1 0 0 1 1.4 0z" />
                      </svg>
                    </button>
                    <div className="rp-detail-nav-spacer" />
                  </div>

                  <div className="rp-detail-header">
                    {activeRedPacket.senderAvatarSrc ? (
                      <img className="rp-detail-avatar" src={activeRedPacket.senderAvatarSrc} alt="avatar" />
                    ) : (
                      <div className="rp-detail-avatar placeholder">{senderName.charAt(0)?.toUpperCase() || 'U'}</div>
                    )}
                    <div className="rp-detail-title">{senderName}的红包</div>
                    <div className="rp-detail-message">{senderMessage}</div>
                  </div>
                </div>

                <div className="rp-detail-body">
                  <div className="rp-detail-summary">1个红包共{activeRedPacket.params.amount.toFixed(2)}元</div>

                  <div className="rp-detail-list">
                    <div className="rp-detail-item">
                      {receiverAvatarSrc ? (
                        <img className="rp-detail-item-avatar" src={receiverAvatarSrc} alt="avatar" />
                      ) : (
                        <div className="rp-detail-item-avatar placeholder">{receiverName.charAt(0)?.toUpperCase() || 'A'}</div>
                      )}
                      <div className="rp-detail-item-main">
                        <div className="rp-detail-item-name">{receiverName}</div>
                        <div className="rp-detail-item-time">{receiverTime || '未领取'}</div>
                      </div>
                      <div className="rp-detail-item-amount">{activeRedPacket.params.amount.toFixed(2)}元</div>
                    </div>
                  </div>
                </div>
              </div>
            )
          }

          const receiverName =
            received?.receiverName ||
            (activeRedPacket.senderRole === 'assistant'
              ? userInfo?.username?.trim() || '你'
              : prompt?.name?.trim() || 'AI Assistant')

          return (
            <div className="rp-modal-overlay">
              <div className={`rp-modal ${packetStep === 'opened' ? 'opened' : ''}`}>
                <button className="rp-close-btn" onClick={closeRedPacketModal}>×</button>

                {packetStep !== 'opened' ? (
                  <div className="rp-modal-front">
                    <div className="rp-modal-top">
                      <div className="rp-sender-row">
                        {activeRedPacket.senderAvatarSrc ? (
                          <img src={activeRedPacket.senderAvatarSrc} className="rp-avatar-img" alt="avatar" />
                        ) : (
                          <div className="rp-avatar-placeholder">
                            {senderName.charAt(0)?.toUpperCase() || 'A'}
                          </div>
                        )}
                        <span className="rp-sender-name">{senderName}</span>
                      </div>
                      <div className="rp-wishing">{senderMessage}</div>
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
                        {activeRedPacket.senderAvatarSrc ? (
                          <img src={activeRedPacket.senderAvatarSrc} className="rp-avatar-img small" alt="avatar" />
                        ) : (
                          <div className="rp-avatar-placeholder small">
                            {senderName.charAt(0)?.toUpperCase() || 'A'}
                          </div>
                        )}
                        <span className="rp-sender-name dark">{senderName}的红包</span>
                      </div>
                      <div className="rp-wishing dark">{senderMessage}</div>
                    </div>

                    <div className="rp-result-amount">
                      <span className="rp-currency">¥</span>
                      <span className="rp-num">{activeRedPacket.params.amount.toFixed(2)}</span>
                    </div>

                    <div className="rp-result-footer">
                      <div className="rp-result-meta">
                        <div className="rp-result-meta-row">
                          <span className="rp-result-meta-label">领取者</span>
                          <span className="rp-result-meta-value">{receiverName}</span>
                        </div>
                        <div className="rp-result-meta-hint">
                          {activeRedPacket.senderRole === 'assistant' ? '已存入零钱，可直接使用' : `已被${receiverName}领取`}
                        </div>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </div>
          )
        })()
      )}

      {redPacketComposerOpen &&
        createPortal(
          <div className="rp-compose-overlay">
            <div className="rp-compose-topbar">
              <button
                type="button"
                className="rp-compose-back"
                onClick={closeRedPacketComposer}
                aria-label="返回"
              >
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M15.5 5.5a1 1 0 0 1 0 1.4L10.4 12l5.1 5.1a1 1 0 1 1-1.4 1.4l-5.8-5.8a1 1 0 0 1 0-1.4l5.8-5.8a1 1 0 0 1 1.4 0z" />
                </svg>
              </button>
              <div className="rp-compose-topbar-title">发红包</div>
              <div className="rp-compose-topbar-spacer" />
            </div>

            <div className="rp-compose-content">
              <div className="rp-compose-form">
                <div className="rp-compose-row">
                  <input
                    ref={redPacketAmountInputRef}
                    className="rp-compose-row-input"
                    type="number"
                    inputMode="decimal"
                    min="0.01"
                    step="0.01"
                    placeholder="单个金额"
                    value={redPacketAmountDraft}
                    onChange={(e) => setRedPacketAmountDraft(e.target.value)}
                  />
                  <div className="rp-compose-row-right">
                    ¥
                    {(() => {
                      const value = Number.parseFloat(redPacketAmountDraft)
                      if (!Number.isFinite(value) || value <= 0) return '0.00'
                      return value.toFixed(2)
                    })()}
                  </div>
                </div>

                <div className="rp-compose-row">
                  <input
                    className="rp-compose-row-input"
                    type="text"
                    placeholder="恭喜发财，大吉大利"
                    value={redPacketBlessingDraft}
                    maxLength={10}
                    onChange={(e) => setRedPacketBlessingDraft(e.target.value)}
                  />
                  <div className="rp-compose-row-right subtle">{redPacketBlessingDraft.length}/10</div>
                </div>
              </div>

              <div className="rp-compose-amount-preview">
                <span className="rp-compose-amount-currency">¥</span>
                <span className="rp-compose-amount-value">
                  {(() => {
                    const value = Number.parseFloat(redPacketAmountDraft)
                    if (!Number.isFinite(value) || value <= 0) return '0.00'
                    return value.toFixed(2)
                  })()}
                </span>
              </div>

              {redPacketComposerError && <div className="rp-compose-error">{redPacketComposerError}</div>}

              <button
                type="button"
                className="rp-compose-send"
                onClick={handleRedPacketComposerSend}
                disabled={sending}
              >
                塞钱进红包
              </button>

              <div className="rp-compose-footnote">未领取的红包，将于24小时后发起退款</div>
            </div>
          </div>,
          document.body
        )}
    </div>
  )
}

export default ChatDetail
