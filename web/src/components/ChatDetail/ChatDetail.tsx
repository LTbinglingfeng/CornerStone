import { useEffect, useMemo, useRef, useState } from 'react'
import { gsap } from 'gsap'
import type { ChatMessage, ToolCall } from '../../types/chat'
import {
  appendQueryParam,
  deleteSessionMessage,
  getChatImageUrl,
  getPromptAvatarUrl,
  getUserAvatarUrl,
  openSessionRedPacket,
  recallSessionMessage,
  updateSessionMessage,
  uploadChatImage,
} from '../../services/api'
import ChatSettings from '../ChatSettings'
import { useToast } from '../../contexts/ToastContext'
import { useConfirm } from '../../contexts/ConfirmContext'
import type { ActiveRedPacketState, ChatDetailProps, MessageEditState, MessageMenuState, QuoteDraft } from './types'
import { useChatSession } from './hooks/useChatSession'
import { useDisplayItems } from './hooks/useDisplayItems'
import { useKeyboardOffset } from './hooks/useKeyboardOffset'
import { useMessageStream } from './hooks/useMessageStream'
import { ChatHeader } from './components/ChatHeader'
import { MessageContextMenu } from './components/MessageContextMenu'
import { MessageEditModal } from './components/MessageEditModal'
import { MessageInput } from './components/MessageInput'
import { MessageList } from './components/MessageList'
import { PendingImages } from './components/PendingImages'
import { SelectTextModal } from './components/SelectTextModal'
import type { PacketStep } from './RedPacket'
import { RedPacketComposer, RedPacketModal, collectOpenedRedPacketKeys, getRedPacketReceivedRecord } from './RedPacket'
import { buildQuoteLineFromMessage, buildQuotedOutgoingContent, parseQuotedMessageContent } from './utils'
import './ChatDetail.css'

const ChatDetail: React.FC<ChatDetailProps> = ({ sessionId, promptId, onBack, onSwitchSession }) => {
  const { showToast } = useToast()
  const { confirm } = useConfirm()

  const { session, setSession, messages, setMessages, prompt, userInfo, loading, imageCapable } = useChatSession({
    sessionId,
    promptId,
  })

  const effectivePromptId = promptId || prompt?.id || session?.prompt_id
  const { sending, streamingTimestamp, revealingTimestamp, assistantVisibleSegments, sendMessage, flushPendingMessages, abortRequest } =
    useMessageStream({
      sessionId,
      promptId: effectivePromptId,
      messages,
      setMessages,
    })

  const displayItems = useDisplayItems({
    messages,
    sending,
    streamingTimestamp,
    revealingTimestamp,
    visibleSegments: assistantVisibleSegments,
  })

  const openedRedPacketKeys = useMemo(() => collectOpenedRedPacketKeys(messages), [messages])

  const [inputText, setInputText] = useState('')
  const [showSettings, setShowSettings] = useState(false)
  const [pendingImages, setPendingImages] = useState<string[]>([])
  const [uploadingImage, setUploadingImage] = useState(false)
  const [showAttachmentMenu, setShowAttachmentMenu] = useState(false)
  const [quoteDraft, setQuoteDraft] = useState<QuoteDraft | null>(null)
  const [messageMenu, setMessageMenu] = useState<MessageMenuState | null>(null)
  const [editState, setEditState] = useState<MessageEditState | null>(null)
  const [selectTextState, setSelectTextState] = useState<string | null>(null)

  const [redPacketComposerOpen, setRedPacketComposerOpen] = useState(false)
  const [activeRedPacket, setActiveRedPacket] = useState<ActiveRedPacketState | null>(null)
  const [packetStep, setPacketStep] = useState<PacketStep>('idle')

  const containerRef = useRef<HTMLDivElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const attachmentButtonRef = useRef<HTMLButtonElement>(null)
  const attachmentPanelRef = useRef<HTMLDivElement>(null)
  const longPressTimeoutRef = useRef<number | null>(null)
  const longPressStartRef = useRef<{ x: number; y: number } | null>(null)
  const redPacketOpenTimeoutRef = useRef<number | null>(null)
  const lastPatAtRef = useRef(0)

  useKeyboardOffset({ containerRef, messageListRef })

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
    if (longPressTimeoutRef.current !== null) {
      window.clearTimeout(longPressTimeoutRef.current)
      longPressTimeoutRef.current = null
    }
    if (redPacketOpenTimeoutRef.current !== null) {
      window.clearTimeout(redPacketOpenTimeoutRef.current)
      redPacketOpenTimeoutRef.current = null
    }
    setInputText('')
    setPendingImages([])
    setUploadingImage(false)
    setShowAttachmentMenu(false)
    setRedPacketComposerOpen(false)
    setQuoteDraft(null)
    setMessageMenu(null)
    setEditState(null)
    setSelectTextState(null)
    setActiveRedPacket(null)
    setPacketStep('idle')
    if (containerRef.current) {
      gsap.set(containerRef.current, { x: '0%' })
    }
  }, [sessionId])

  const scrollToBottom = () => {
    if (messageListRef.current) {
      messageListRef.current.scrollTop = messageListRef.current.scrollHeight
    }
  }

  const getUserAvatarSrc = () => {
    if (userInfo?.avatar) {
      return appendQueryParam(getUserAvatarUrl(), 't', new Date(userInfo.updated_at).getTime())
    }
    return null
  }

  const getPromptAvatarSrc = () => {
    if (prompt?.avatar) {
      return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
    }
    return null
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

  const handleMessageContextMenu = (e: React.MouseEvent, item: { type: string; messageIndex: number; message: ChatMessage }) => {
    if (item.type !== 'text') return
    e.preventDefault()
    openMessageMenuAt({ x: e.clientX, y: e.clientY }, item.messageIndex, item.message)
  }

  const handleMessagePointerDown = (e: React.PointerEvent, item: { type: string; messageIndex: number; message: ChatMessage }) => {
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
      showToast('编辑失败，请重试', 'error')
      return
    }
    setSession(updated)
    setMessages(updated.messages || [])
    setEditState(null)
  }

  const handleDeleteMessage = async (messageIndex: number) => {
    const ok = await confirm({
      title: '删除消息',
      message: '确定删除这条消息吗？',
      confirmText: '删除',
      danger: true,
    })
    if (!ok) return

    const updated = await deleteSessionMessage(sessionId, messageIndex)
    if (!updated) {
      showToast('删除失败，请重试', 'error')
      return
    }
    setSession(updated)
    setMessages(updated.messages || [])
    setMessageMenu(null)
  }

  const getRoleDisplayName = (role: string) => {
    if (role === 'assistant') return prompt?.name || 'AI'
    return userInfo?.username || '我'
  }

  const handleQuoteMessage = (message: ChatMessage) => {
    setQuoteDraft({ line: buildQuoteLineFromMessage(message, getRoleDisplayName) })
    setMessageMenu(null)
    textareaRef.current?.focus()
  }

  const handleRecallMessage = async (messageIndex: number) => {
    const updated = await recallSessionMessage(sessionId, messageIndex)
    if (!updated) {
      showToast('撤回失败，请重试', 'error')
      return
    }
    setSession(updated)
    setMessages(updated.messages || [])
    setMessageMenu(null)
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
      showToast('图片上传失败', 'error')
    }

    setUploadingImage(false)
    e.target.value = ''
  }

  const handleUploadClick = () => {
    if (!imageCapable || uploadingImage) return
    fileInputRef.current?.click()
  }

  const handleSend = async () => {
    const trimmedText = inputText.trim()
    const hasImages = pendingImages.length > 0
    if ((!trimmedText && !hasImages) || sending || uploadingImage) return
    if (hasImages && !imageCapable) {
      showToast('当前模型不支持图片输入', 'error')
      return
    }

    const finalText = quoteDraft ? buildQuotedOutgoingContent(quoteDraft.line, trimmedText) : trimmedText
    const userMessage: ChatMessage = {
      role: 'user',
      content: finalText,
      timestamp: new Date().toISOString(),
      ...(hasImages ? { image_paths: pendingImages } : {}),
    }

    setInputText('')
    setQuoteDraft(null)
    setPendingImages([])
    setShowAttachmentMenu(false)
    if (textareaRef.current) {
      textareaRef.current.style.height = '36px'
    }

    await sendMessage(userMessage)
  }

  const handleBack = () => {
    if (!sending) {
      void flushPendingMessages('background')
    }

    abortRequest()
    if (containerRef.current) {
      gsap.to(containerRef.current, {
        x: '100%',
        duration: 0.3,
        ease: 'power2.in',
        onComplete: onBack,
      })
    } else {
      onBack()
    }
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

    void sendMessage(userMessage)
  }

  const openRedPacketComposer = () => {
    if (sending) return
    setShowAttachmentMenu(false)
    setRedPacketComposerOpen(true)
  }

  const handleRedPacketComposerSend = (params: { amount: number; message: string }) => {
    if (sending) return

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
        arguments: JSON.stringify(params),
      },
    }

    const userMessage: ChatMessage = {
      role: 'user',
      content: '',
      timestamp: new Date().toISOString(),
      tool_calls: [toolCall],
    }

    void sendMessage(userMessage)
  }

  const closeRedPacketModal = () => {
    if (redPacketOpenTimeoutRef.current !== null) {
      window.clearTimeout(redPacketOpenTimeoutRef.current)
      redPacketOpenTimeoutRef.current = null
    }
    setActiveRedPacket(null)
    setPacketStep('idle')
  }

  const handleOpenPacket = () => {
    if (!activeRedPacket) return
    if (packetStep !== 'idle') return

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
        const updated = await openSessionRedPacket(sessionId, activeRedPacket.packetKey, receiverName, senderName)
        if (!updated) return
        setSession(updated)
        setMessages(updated.messages || [])
      })()
    }, 1000)
  }

  const canSend = (inputText.trim() !== '' || pendingImages.length > 0) && !sending && !uploadingImage
  const userAvatarSrc = getUserAvatarSrc()
  const assistantAvatarSrc = getPromptAvatarSrc()
  const userPlaceholder = userInfo?.username?.charAt(0)?.toUpperCase() || 'U'
  const assistantPlaceholder = prompt?.name?.charAt(0)?.toUpperCase() || 'A'
  const userDisplayName = userInfo?.username?.trim() || '我'
  const assistantDisplayName = prompt?.name?.trim() || 'AI Assistant'

  return (
    <div className="chat-detail" ref={containerRef}>
      <ChatHeader
        title={session?.title || '对话'}
        sending={sending}
        assistantVisibleSegments={assistantVisibleSegments}
        showSettingsButton={!!prompt}
        onBack={handleBack}
        onOpenSettings={() => setShowSettings(true)}
      />

      <MessageList
        sessionId={sessionId}
        loading={loading}
        messages={messages}
        userInfo={userInfo}
        prompt={prompt}
        listRef={messageListRef}
        displayItems={displayItems}
        openedRedPacketKeys={openedRedPacketKeys}
        getImageUrl={getChatImageUrl}
        userAvatarSrc={userAvatarSrc}
        assistantAvatarSrc={assistantAvatarSrc}
        userPlaceholder={userPlaceholder}
        assistantPlaceholder={assistantPlaceholder}
        userDisplayName={userDisplayName}
        assistantDisplayName={assistantDisplayName}
        onPatAssistant={handlePatAssistant}
        onOpenRedPacket={(active, initialStep) => {
          setActiveRedPacket(active)
          setPacketStep(initialStep)
        }}
        onContextMenu={handleMessageContextMenu}
        onPointerDown={handleMessagePointerDown}
        onPointerMove={handleMessagePointerMove}
        onPointerUp={handleMessagePointerUp}
        onPointerCancel={handleMessagePointerUp}
        onPointerLeave={handleMessagePointerUp}
      />

      <PendingImages
        pendingImages={pendingImages}
        getImageUrl={getChatImageUrl}
        onRemove={index => setPendingImages(prev => prev.filter((_, i) => i !== index))}
      />

      <MessageInput
        value={inputText}
        onChange={setInputText}
        onSend={handleSend}
        canSend={canSend}
        quoteDraft={quoteDraft}
        onClearQuote={() => setQuoteDraft(null)}
        showAttachmentMenu={showAttachmentMenu}
        onToggleAttachmentMenu={() => setShowAttachmentMenu(prev => !prev)}
        onCloseAttachmentMenu={() => setShowAttachmentMenu(false)}
        onUploadClick={handleUploadClick}
        onOpenRedPacket={openRedPacketComposer}
        onImageChange={handleImageChange}
        imageCapable={imageCapable}
        uploadingImage={uploadingImage}
        sending={sending}
        textareaRef={textareaRef}
        fileInputRef={fileInputRef}
        attachmentButtonRef={attachmentButtonRef}
        attachmentPanelRef={attachmentPanelRef}
        onFocusInput={scrollToBottom}
      />

      {messageMenu && (
        <MessageContextMenu
          state={messageMenu}
          sending={sending}
          onClose={() => setMessageMenu(null)}
          onSelectText={text => setSelectTextState(text)}
          onRecall={handleRecallMessage}
          onEdit={handleStartEditMessage}
          onDelete={handleDeleteMessage}
          onQuote={handleQuoteMessage}
        />
      )}

      {selectTextState && <SelectTextModal text={selectTextState} onClose={() => setSelectTextState(null)} />}

      {editState && (
        <MessageEditModal
          state={editState}
          onClose={() => setEditState(null)}
          onChangeText={text => setEditState(prev => (prev ? { ...prev, text } : prev))}
          onSave={handleSaveEditMessage}
          saveDisabled={!editState.quoteLine && editState.text.trim() === ''}
        />
      )}

      {showSettings && prompt && (
        <ChatSettings
          prompt={prompt}
          currentSessionId={sessionId}
          currentSessionTitle={session?.title}
          onClose={() => setShowSettings(false)}
          onSwitchSession={newSessionId => {
            setShowSettings(false)
            onSwitchSession?.(newSessionId, prompt.id)
          }}
          onTitleUpdated={newTitle => {
            setSession(prev => (prev ? { ...prev, title: newTitle } : prev))
          }}
          onExitChat={handleBack}
        />
      )}

      {activeRedPacket && (
        <RedPacketModal
          activeRedPacket={activeRedPacket}
          packetStep={packetStep}
          onOpen={handleOpenPacket}
          onClose={closeRedPacketModal}
          userInfo={userInfo}
          prompt={prompt}
          getReceivedRecord={packetKey => getRedPacketReceivedRecord(messages, packetKey)}
        />
      )}

      <RedPacketComposer
        open={redPacketComposerOpen}
        sending={sending}
        onClose={() => setRedPacketComposerOpen(false)}
        onSend={handleRedPacketComposerSend}
      />
    </div>
  )
}

export default ChatDetail
