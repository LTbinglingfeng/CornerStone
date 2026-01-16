import { useCallback, useEffect, useState } from 'react'
import type { ChatMessage, ChatRecord, Prompt, UserInfo } from '../../../types/chat'
import { getActiveProvider, getPrompt, getSession, getUserInfo } from '../../../services/api'

interface UseChatSessionOptions {
    sessionId: string
    promptId?: string
}

interface UseChatSessionReturn {
    session: ChatRecord | null
    setSession: React.Dispatch<React.SetStateAction<ChatRecord | null>>
    messages: ChatMessage[]
    setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>
    prompt: Prompt | null
    userInfo: UserInfo | null
    loading: boolean
    imageCapable: boolean
    reload: () => Promise<void>
}

export function useChatSession(options: UseChatSessionOptions): UseChatSessionReturn {
    const { sessionId, promptId } = options
    const [session, setSession] = useState<ChatRecord | null>(null)
    const [messages, setMessages] = useState<ChatMessage[]>([])
    const [prompt, setPrompt] = useState<Prompt | null>(null)
    const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
    const [loading, setLoading] = useState(false)
    const [imageCapable, setImageCapable] = useState(false)

    const reload = useCallback(async () => {
        setLoading(true)
        const data = await getSession(sessionId)
        if (data) {
            setSession(data)
            setMessages(data.messages || [])

            const effectivePromptId = promptId || data.prompt_id
            if (effectivePromptId) {
                const promptData = await getPrompt(effectivePromptId)
                if (promptData) {
                    setPrompt(promptData)
                } else {
                    setPrompt(null)
                }
            } else {
                setPrompt(null)
            }
        } else {
            setSession(null)
            setMessages([])
            setPrompt(null)
        }

        const user = await getUserInfo()
        setUserInfo(user || null)

        const provider = await getActiveProvider()
        setImageCapable(!!provider?.image_capable)

        setLoading(false)
    }, [promptId, sessionId])

    useEffect(() => {
        let cancelled = false
        const run = async () => {
            await reload()
            if (cancelled) return
        }
        void run()
        return () => {
            cancelled = true
        }
    }, [reload])

    return { session, setSession, messages, setMessages, prompt, userInfo, loading, imageCapable, reload }
}
