import { useCallback, useEffect, useRef, useState } from 'react'
import type { ChatMessage, ChatRecord, Prompt, UserInfo } from '../../../types/chat'
import { getActiveProvider, getPrompt, getSessionMessagesPage, getUserInfo } from '../../../services/api'

interface UseChatSessionOptions {
    sessionId: string
    promptId?: string
}

interface UseChatSessionReturn {
    session: ChatRecord | null
    setSession: React.Dispatch<React.SetStateAction<ChatRecord | null>>
    messages: ChatMessage[]
    setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>
    messagesOffset: number
    setMessagesOffset: React.Dispatch<React.SetStateAction<number>>
    messagesTotal: number
    setMessagesTotal: React.Dispatch<React.SetStateAction<number>>
    loadingOlder: boolean
    prompt: Prompt | null
    userInfo: UserInfo | null
    loading: boolean
    imageCapable: boolean
    reload: () => Promise<void>
    loadOlder: () => Promise<boolean>
}

export function useChatSession(options: UseChatSessionOptions): UseChatSessionReturn {
    const { sessionId, promptId } = options
    const [session, setSession] = useState<ChatRecord | null>(null)
    const [messages, setMessages] = useState<ChatMessage[]>([])
    const [messagesOffset, setMessagesOffset] = useState(0)
    const [messagesTotal, setMessagesTotal] = useState(0)
    const [loadingOlder, setLoadingOlder] = useState(false)
    const [prompt, setPrompt] = useState<Prompt | null>(null)
    const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
    const [loading, setLoading] = useState(false)
    const [imageCapable, setImageCapable] = useState(false)
    const requestTokenRef = useRef(0)

    const pageSize = 60

    const resetSessionState = useCallback(() => {
        setSession(null)
        setMessages([])
        setMessagesOffset(0)
        setMessagesTotal(0)
        setPrompt(null)
    }, [])

    const reload = useCallback(async () => {
        const requestToken = requestTokenRef.current + 1
        requestTokenRef.current = requestToken
        setLoading(true)

        try {
            const data = await getSessionMessagesPage(sessionId, { limit: pageSize })
            if (requestToken !== requestTokenRef.current) return

            if (!data) {
                resetSessionState()
            } else {
                setSession(data)
                const pageMessages = data.messages || []
                const total = typeof data.messages_total === 'number' ? data.messages_total : pageMessages.length
                const offset =
                    typeof data.messages_offset === 'number'
                        ? data.messages_offset
                        : Math.max(0, total - pageMessages.length)
                setMessages(pageMessages)
                setMessagesOffset(offset)
                setMessagesTotal(total)

                const effectivePromptId = promptId || data.prompt_id
                if (effectivePromptId) {
                    try {
                        const promptData = await getPrompt(effectivePromptId)
                        if (requestToken !== requestTokenRef.current) return
                        setPrompt(promptData)
                    } catch {
                        if (requestToken !== requestTokenRef.current) return
                        setPrompt(null)
                    }
                } else {
                    setPrompt(null)
                }
            }

            const [user, provider] = await Promise.all([getUserInfo(), getActiveProvider()])
            if (requestToken !== requestTokenRef.current) return

            setUserInfo(user || null)
            setImageCapable(!!provider?.image_capable)
        } finally {
            if (requestToken === requestTokenRef.current) {
                setLoading(false)
            }
        }
    }, [pageSize, promptId, resetSessionState, sessionId])

    const loadOlder = useCallback(async () => {
        if (loading || loadingOlder) return false
        if (messagesOffset <= 0) return false

        const requestToken = requestTokenRef.current
        setLoadingOlder(true)
        try {
            const before = messagesOffset
            const data = await getSessionMessagesPage(sessionId, { limit: pageSize, before })
            if (requestToken !== requestTokenRef.current) return false
            if (!data) return false

            const olderMessages = data.messages || []
            if (olderMessages.length === 0) return false

            const total =
                typeof data.messages_total === 'number' ? data.messages_total : Math.max(messagesTotal, before)
            const offset =
                typeof data.messages_offset === 'number'
                    ? data.messages_offset
                    : Math.max(0, before - olderMessages.length)

            setMessages((prev) => [...olderMessages, ...prev])
            setMessagesOffset(offset)
            setMessagesTotal(total)
            setSession((prev) => (prev ? { ...prev, ...data, messages: prev.messages } : data))
            return true
        } finally {
            setLoadingOlder(false)
        }
    }, [loading, loadingOlder, messagesOffset, messagesTotal, pageSize, sessionId])

    useEffect(() => {
        resetSessionState()
        setLoadingOlder(false)
        void reload()
        return () => {
            requestTokenRef.current += 1
        }
    }, [reload, resetSessionState])

    return {
        session,
        setSession,
        messages,
        setMessages,
        messagesOffset,
        setMessagesOffset,
        messagesTotal,
        setMessagesTotal,
        loadingOlder,
        prompt,
        userInfo,
        loading,
        imageCapable,
        reload,
        loadOlder,
    }
}
