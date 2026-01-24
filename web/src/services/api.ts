import type {
    ApiResponse,
    ChatSession,
    ChatRecord,
    AppConfig,
    Provider,
    ProvidersResponse,
    Prompt,
    UserInfo,
    AuthStatus,
    AuthSession,
    ToolCall,
} from '../types/chat'

const API_BASE = '/api'
const MANAGEMENT_BASE = '/management'
const AUTH_TOKEN_KEY = 'cornerstone.auth.token'

export function getAuthToken(): string | null {
    return localStorage.getItem(AUTH_TOKEN_KEY)
}

export function setAuthToken(token: string | null): void {
    if (token) {
        localStorage.setItem(AUTH_TOKEN_KEY, token)
    } else {
        localStorage.removeItem(AUTH_TOKEN_KEY)
    }
}

export function appendQueryParam(url: string, key: string, value: string | number): string {
    const separator = url.includes('?') ? '&' : '?'
    return `${url}${separator}${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`
}

async function apiFetch(url: string, init: RequestInit = {}): Promise<Response> {
    const headers = new Headers(init.headers || {})
    const token = getAuthToken()
    if (token && !headers.has('Authorization')) {
        headers.set('Authorization', `Bearer ${token}`)
    }

    const res = await fetch(url, { ...init, headers })
    if (res.ok) return res

    let message = `${res.status} ${res.statusText}`.trim()
    try {
        const contentType = res.headers.get('Content-Type') || ''
        if (contentType.includes('application/json')) {
            const body = (await res.json()) as Partial<ApiResponse<unknown>> & { message?: string; error?: string }
            message = body.error || body.message || message
        } else {
            const text = await res.text()
            if (text) message = text
        }
    } catch {
        // ignore body parse errors
    }

    const error = new Error(message)
    ;(error as Error & { status?: number; url?: string }).status = res.status
    ;(error as Error & { status?: number; url?: string }).url = url
    throw error
}

export async function apiFetchJson<T>(url: string, init?: RequestInit): Promise<T> {
    const res = await apiFetch(url, init)
    return (await res.json()) as T
}

export async function getAuthStatus(): Promise<AuthStatus | null> {
    try {
        const data = await apiFetchJson<ApiResponse<AuthStatus>>(`${MANAGEMENT_BASE}/auth/status`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function setupAuth(username: string, password: string): Promise<AuthSession> {
    const data = await apiFetchJson<ApiResponse<AuthSession>>(`${MANAGEMENT_BASE}/auth/setup`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
    })
    if (!data.success || !data.data) {
        throw new Error(data.error || 'Setup failed')
    }
    return data.data
}

export async function loginAuth(username: string, password: string): Promise<AuthSession> {
    const data = await apiFetchJson<ApiResponse<AuthSession>>(`${MANAGEMENT_BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
    })
    if (!data.success || !data.data) {
        throw new Error(data.error || 'Login failed')
    }
    return data.data
}

export async function getSessions(): Promise<ChatSession[]> {
    try {
        const data = await apiFetchJson<ApiResponse<ChatSession[]>>(`${MANAGEMENT_BASE}/sessions`)
        return data.success && data.data ? data.data : []
    } catch {
        return []
    }
}

export async function createSession(title?: string, promptId?: string): Promise<ChatRecord | null> {
    try {
        const data = await apiFetchJson<ApiResponse<ChatRecord>>(`${MANAGEMENT_BASE}/sessions`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                title: title || 'New Chat',
                prompt_id: promptId,
            }),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function getSession(id: string): Promise<ChatRecord | null> {
    try {
        const data = await apiFetchJson<ApiResponse<ChatRecord>>(`${MANAGEMENT_BASE}/sessions/${id}`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export interface GetSessionMessagesPageOptions {
    limit?: number
    before?: number
}

export async function getSessionMessagesPage(id: string, options: GetSessionMessagesPageOptions = {}): Promise<ChatRecord | null> {
    try {
        const params = new URLSearchParams()
        if (options.limit !== undefined) {
            params.set('limit', String(options.limit))
        }
        if (options.before !== undefined) {
            params.set('before', String(options.before))
        }
        const suffix = params.toString() ? `?${params.toString()}` : ''
        const data = await apiFetchJson<ApiResponse<ChatRecord>>(`${MANAGEMENT_BASE}/sessions/${id}/messages${suffix}`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function deleteSession(id: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/sessions/${id}`, { method: 'DELETE' })
        return data.success
    } catch {
        return false
    }
}

// 更新会话标题
export async function updateSessionTitle(id: string, title: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/sessions/${id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ title }),
        })
        return data.success
    } catch {
        return false
    }
}

export async function updateSessionMessage(id: string, index: number, content: string): Promise<ChatRecord | null> {
    try {
        const data = await apiFetchJson<ApiResponse<ChatRecord>>(`${MANAGEMENT_BASE}/sessions/${id}/messages/update`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ index, content }),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function deleteSessionMessage(id: string, index: number): Promise<ChatRecord | null> {
    try {
        const data = await apiFetchJson<ApiResponse<ChatRecord>>(`${MANAGEMENT_BASE}/sessions/${id}/messages/delete`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ index }),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function recallSessionMessage(id: string, index: number): Promise<ChatRecord | null> {
    try {
        const data = await apiFetchJson<ApiResponse<ChatRecord>>(`${MANAGEMENT_BASE}/sessions/${id}/messages/recall`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ index }),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function openSessionRedPacket(
    id: string,
    packetKey: string,
    receiverName?: string,
    senderName?: string
): Promise<ChatRecord | null> {
    try {
        const data = await apiFetchJson<ApiResponse<ChatRecord>>(
            `${MANAGEMENT_BASE}/sessions/${id}/messages/red-packet-open`,
            {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ packet_key: packetKey, receiver_name: receiverName, sender_name: senderName }),
            }
        )
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 根据提示词ID获取所有会话
export async function getSessionsByPromptId(promptId: string): Promise<ChatSession[]> {
    try {
        const data = await apiFetchJson<ApiResponse<ChatSession[]>>(`${MANAGEMENT_BASE}/prompts-sessions/${promptId}`)
        return data.success && data.data ? data.data : []
    } catch {
        return []
    }
}

export interface SendMessageOptions {
    promptId?: string
    stream?: boolean
    saveHistory?: boolean
    signal?: AbortSignal
    keepalive?: boolean
}

export async function sendMessage(
    sessionId: string,
    messages: { role: string; content: string; image_paths?: string[]; tool_calls?: ToolCall[] }[],
    options: SendMessageOptions = {}
): Promise<Response> {
    const payload: Record<string, unknown> = {
        session_id: sessionId,
        prompt_id: options.promptId,
        messages,
        save_history: options.saveHistory ?? true,
    }

    if (options.stream !== undefined) {
        payload.stream = options.stream
    }

    const init: RequestInit & { keepalive?: boolean } = {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
        signal: options.signal,
    }
    if (options.keepalive !== undefined) {
        init.keepalive = options.keepalive
    }

    return apiFetch(`${API_BASE}/chat`, init)
}

export function sendMessageBeacon(
    sessionId: string,
    messages: { role: string; content: string; image_paths?: string[]; tool_calls?: ToolCall[] }[],
    options: Omit<SendMessageOptions, 'signal'> = {}
): boolean {
    const payload: Record<string, unknown> = {
        session_id: sessionId,
        prompt_id: options.promptId,
        messages,
        save_history: options.saveHistory ?? true,
    }

    if (options.stream !== undefined) {
        payload.stream = options.stream
    }

    const url = `${API_BASE}/chat`
    const body = JSON.stringify(payload)
    const token = getAuthToken()

    try {
        if (typeof navigator !== 'undefined' && typeof navigator.sendBeacon === 'function') {
            const blob = new Blob([body], { type: 'application/json' })
            return navigator.sendBeacon(url, blob)
        }
    } catch {
        // ignore beacon errors
    }

    try {
        const headers: Record<string, string> = { 'Content-Type': 'application/json' }
        if (token) {
            headers.Authorization = `Bearer ${token}`
        }
        const init: RequestInit & { keepalive?: boolean } = {
            method: 'POST',
            headers,
            body,
            keepalive: options.keepalive ?? true,
        }
        void fetch(url, init)
    } catch {
        // ignore fallback errors
    }

    return false
}

export async function healthCheck(): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/health`)
        return data.success
    } catch {
        return false
    }
}

// 兼容旧版配置 API
export async function getConfig(): Promise<AppConfig | null> {
    try {
        const data = await apiFetchJson<ApiResponse<AppConfig>>(`${MANAGEMENT_BASE}/config`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function updateConfig(config: Partial<AppConfig>): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/config`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(config),
        })
        return data.success
    } catch {
        return false
    }
}

// ========== 供应商管理 API ==========

// 获取所有供应商
export async function getProviders(): Promise<ProvidersResponse | null> {
    try {
        const data = await apiFetchJson<ApiResponse<ProvidersResponse>>(`${MANAGEMENT_BASE}/providers`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 获取当前激活供应商
export async function getActiveProvider(): Promise<Provider | null> {
    try {
        const data = await apiFetchJson<ApiResponse<Provider>>(`${MANAGEMENT_BASE}/providers/active`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 添加新供应商
export async function addProvider(provider: Provider): Promise<Provider | null> {
    try {
        const data = await apiFetchJson<ApiResponse<Provider>>(`${MANAGEMENT_BASE}/providers`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(provider),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 更新供应商
export async function updateProvider(provider: Provider): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<Provider>>(`${MANAGEMENT_BASE}/providers/${provider.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(provider),
        })
        return data.success
    } catch {
        return false
    }
}

// 删除供应商
export async function deleteProvider(id: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/providers/${id}`, { method: 'DELETE' })
        return data.success
    } catch {
        return false
    }
}

// 设置激活的供应商
export async function setActiveProvider(providerId: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/providers/active`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ provider_id: providerId }),
        })
        return data.success
    } catch {
        return false
    }
}

export async function setImageProvider(providerId: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<{ image_provider_id: string }>>(`${MANAGEMENT_BASE}/image-provider`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ provider_id: providerId }),
        })
        return data.success
    } catch {
        return false
    }
}

export async function updateMemoryProvider(
    useCustom: boolean,
    provider?: Provider
): Promise<Provider | null | undefined> {
    try {
        const data = await apiFetchJson<ApiResponse<{ memory_provider: Provider | null }>>(
            `${MANAGEMENT_BASE}/memory-provider`,
            {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ use_custom: useCustom, provider }),
            }
        )
        if (!data.success || !data.data) return undefined
        return data.data.memory_provider || null
    } catch {
        return undefined
    }
}

// 更新系统提示词
export async function updateSystemPrompt(systemPrompt: string): Promise<boolean> {
    return updateConfig({ system_prompt: systemPrompt })
}

// ========== 提示词管理 API ==========

// 获取所有提示词
export async function getPrompts(): Promise<Prompt[]> {
    try {
        const data = await apiFetchJson<ApiResponse<Prompt[]>>(`${MANAGEMENT_BASE}/prompts`)
        return data.success && data.data ? data.data : []
    } catch {
        return []
    }
}

// 获取单个提示词
export async function getPrompt(id: string): Promise<Prompt | null> {
    try {
        const data = await apiFetchJson<ApiResponse<Prompt>>(`${MANAGEMENT_BASE}/prompts/${id}`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 创建提示词
export async function createPrompt(prompt: Partial<Prompt>): Promise<Prompt | null> {
    try {
        const data = await apiFetchJson<ApiResponse<Prompt>>(`${MANAGEMENT_BASE}/prompts`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(prompt),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 更新提示词
export async function updatePrompt(id: string, prompt: Partial<Prompt>): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<Prompt>>(`${MANAGEMENT_BASE}/prompts/${id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(prompt),
        })
        return data.success
    } catch {
        return false
    }
}

// 删除提示词
export async function deletePrompt(id: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/prompts/${id}`, { method: 'DELETE' })
        return data.success
    } catch {
        return false
    }
}

// 上传提示词头像
export async function uploadPromptAvatar(id: string, file: File): Promise<string | null> {
    try {
        const formData = new FormData()
        formData.append('avatar', file)

        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/prompts-avatar/${id}`, {
            method: 'POST',
            body: formData,
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 获取提示词头像URL
export function getPromptAvatarUrl(id: string): string {
    return `${MANAGEMENT_BASE}/prompts-avatar/${id}`
}

// 删除提示词头像
export async function deletePromptAvatar(id: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/prompts-avatar/${id}`, {
            method: 'DELETE',
        })
        return data.success
    } catch {
        return false
    }
}

// ========== 用户信息管理 API ==========

// 获取用户信息
export async function getUserInfo(): Promise<UserInfo | null> {
    try {
        const data = await apiFetchJson<ApiResponse<UserInfo>>(`${MANAGEMENT_BASE}/user`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 更新用户信息
export async function updateUserInfo(info: Partial<UserInfo>): Promise<UserInfo | null> {
    try {
        const data = await apiFetchJson<ApiResponse<UserInfo>>(`${MANAGEMENT_BASE}/user`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(info),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 上传用户头像
export async function uploadUserAvatar(file: File): Promise<string | null> {
    try {
        const formData = new FormData()
        formData.append('avatar', file)

        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/user/avatar`, {
            method: 'POST',
            body: formData,
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 获取用户头像URL
export function getUserAvatarUrl(): string {
    return `${MANAGEMENT_BASE}/user/avatar`
}

// 删除用户头像
export async function deleteUserAvatar(): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/user/avatar`, { method: 'DELETE' })
        return data.success
    } catch {
        return false
    }
}

// 上传聊天图片
export async function uploadChatImage(file: File): Promise<string | null> {
    try {
        const formData = new FormData()
        formData.append('image', file)

        const data = await apiFetchJson<ApiResponse<string>>(`${MANAGEMENT_BASE}/cache-photo`, {
            method: 'POST',
            body: formData,
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

// 获取聊天图片 URL
export function getChatImageUrl(imagePath: string): string {
    const normalized = imagePath.replace(/\\/g, '/')
    const cleaned = normalized.startsWith('cache_photo/') ? normalized.slice('cache_photo/'.length) : normalized
    const encoded = encodeURIComponent(cleaned)
    return `${MANAGEMENT_BASE}/cache-photo/${encoded}`
}

// 获取 TTS 音频 URL
export function getTTSAudioUrl(audioPath: string): string {
    const normalized = audioPath.replace(/\\/g, '/')
    const cleaned = normalized.startsWith('tts_audio/') ? normalized.slice('tts_audio/'.length) : normalized
    const encoded = encodeURIComponent(cleaned)
    return `${MANAGEMENT_BASE}/tts-audio/${encoded}`
}
