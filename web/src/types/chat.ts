export interface ChatMessage {
    role: string
    content: string
    reasoning_content?: string
    timestamp: string
    tool_calls?: ToolCall[]
    image_paths?: string[]
}

// 工具调用
export interface ToolCall {
  id: string
  type: string
  function: {
    name: string
    arguments: string // JSON字符串
  }
}

// 红包参数
export interface RedPacketParams {
  amount: number
  message: string
}

// 拍一拍参数
export interface PatParams {
  name: string
  target?: string
}

export interface ChatSession {
  id: string
  title: string
  prompt_id?: string
  prompt_name?: string
  created_at: string
  updated_at: string
}

export interface ChatRecord {
  id: string
  session_id: string
  title: string
  prompt_id?: string
  prompt_name?: string
  messages: ChatMessage[]
  model?: string
  system_prompt?: string
  created_at: string
  updated_at: string
}

export interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: string
}

export interface AuthStatus {
  needs_setup: boolean
  username?: string
  user_id?: string
  authenticated?: boolean
}

export interface AuthSession {
  token: string
  username: string
  user_id: string
}

// 供应商类型
export type ProviderType = 'openai' | 'openai_response' | 'gemini' | 'anthropic'

// 供应商配置
export interface Provider {
    id: string
    name: string
    type: ProviderType  // 供应商类型 (openai/openai_response/gemini/anthropic)
    base_url: string
    api_key: string
    model: string
    temperature: number
    top_p: number
    thinking_budget: number
    reasoning_effort: string
    gemini_thinking_mode?: string
    gemini_thinking_level?: string
    gemini_thinking_budget?: number
    context_messages: number
    stream: boolean     // 是否启用流式输出
    image_capable: boolean // 是否支持识图
}

// 供应商列表响应
export interface ProvidersResponse {
  providers: Provider[]
  active_provider_id: string
  system_prompt: string
}

// 兼容旧版配置（用于简单场景）
export interface AppConfig {
  base_url: string
  api_key: string
  model: string
  system_prompt: string
}

// 提示词模板
export interface Prompt {
  id: string
  name: string
  content: string
  description?: string
  file_name?: string
  avatar?: string
  created_at: string
  updated_at: string
}

// 用户信息
export interface UserInfo {
  username: string
  description: string
  avatar?: string
  updated_at: string
}
