import { useEffect, useMemo, useRef, useState } from 'react'
import { gsap } from 'gsap'
import type { Provider, ProviderType } from '../types/chat'
import { getProviders, updateMemoryProvider } from '../services/api'
import './ProviderSettings.css'

const PROVIDER_TYPES: { value: ProviderType; label: string }[] = [
  { value: 'openai', label: 'OpenAI 兼容' },
  { value: 'openai_response', label: 'OpenAI Responses' },
  { value: 'gemini', label: 'Google Gemini' },
  { value: 'anthropic', label: 'Anthropic Claude' },
]

const OPENAI_REASONING_EFFORT_OPTIONS = [
  { value: '', label: '默认' },
  { value: 'low', label: '低 (low)' },
  { value: 'medium', label: '中 (medium)' },
  { value: 'high', label: '高 (high)' },
]

const GEMINI_THINKING_MODES = [
  { value: 'none', label: '不思考' },
  { value: 'thinking_level', label: 'thinkingLevel (Gemini 3 系列)' },
  { value: 'thinking_budget', label: 'thinkingBudget (Gemini 2.5 系列)' },
]

const GEMINI_THINKING_LEVELS = [
  { value: 'low', label: '低 (low)' },
  { value: 'high', label: '高 (high)' },
]

const getGeminiThinkingBudgetRange = (model: string): { min: number; max: number } => {
  const normalized = (model || '').trim().toLowerCase()

  // Based on https://ai.google.dev/gemini-api/docs/thinking
  if (normalized.includes('flash-lite')) return { min: 512, max: 24576 }
  if (normalized.includes('flash')) return { min: 0, max: 24576 }
  if (normalized.includes('pro')) return { min: 128, max: 32768 }
  if (normalized.includes('robotics-er')) return { min: 0, max: 24576 }
  return { min: 128, max: 32768 }
}

const clampGeminiThinkingBudget = (model: string, budget: number): number => {
  if (budget === -1) return -1
  const { min, max } = getGeminiThinkingBudgetRange(model)
  return Math.min(Math.max(budget, min), max)
}

interface MemoryProviderSettingsProps {
  onBack: () => void
}

const MemoryProviderSettings: React.FC<MemoryProviderSettingsProps> = ({ onBack }) => {
  const [providers, setProviders] = useState<Provider[]>([])
  const [activeProviderId, setActiveProviderId] = useState('')
  const [memoryProvider, setMemoryProvider] = useState<Provider | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState('')
  const [showModal, setShowModal] = useState(false)
  const [editingProvider, setEditingProvider] = useState<Provider | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const modalRef = useRef<HTMLDivElement>(null)

  const emptyProvider: Provider = {
    id: 'memory',
    name: '记忆提供商',
    type: 'openai',
    base_url: '',
    api_key: '',
    model: '',
    temperature: 0.8,
    top_p: 1,
    thinking_budget: 0,
    reasoning_effort: '',
    gemini_thinking_mode: 'none',
    gemini_thinking_level: 'low',
    gemini_thinking_budget: 128,
    context_messages: 64,
    stream: true,
    image_capable: false,
  }

  useEffect(() => {
    if (containerRef.current) {
      gsap.fromTo(
        containerRef.current,
        { x: '100%' },
        { x: '0%', duration: 0.3, ease: 'power2.out' }
      )
    }
    loadData()
  }, [])

  useEffect(() => {
    if (showModal && modalRef.current) {
      gsap.fromTo(
        modalRef.current,
        { opacity: 0, scale: 0.9 },
        { opacity: 1, scale: 1, duration: 0.2, ease: 'power2.out' }
      )
    }
  }, [showModal])

  const activeChatProvider = useMemo(() => {
    return providers.find((p) => p.id === activeProviderId) || null
  }, [providers, activeProviderId])

  const loadData = async () => {
    setLoading(true)
    const data = await getProviders()
    if (data) {
      setProviders(data.providers || [])
      setActiveProviderId(data.active_provider_id)
      setMemoryProvider(data.memory_provider || null)
    }
    setLoading(false)
  }

  const handleBack = () => {
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

  const showMessageToast = (msg: string) => {
    setMessage(msg)
    setTimeout(() => setMessage(''), 2000)
  }

  const handleUseFollowChat = async () => {
    if (saving) return
    setSaving(true)
    const updated = await updateMemoryProvider(false)
    if (updated !== undefined) {
      setMemoryProvider(null)
      showMessageToast('已切换为跟随对话模型')
    } else {
      showMessageToast('切换失败')
    }
    setSaving(false)
  }

  const openEditModal = () => {
    const base = memoryProvider
      ? {
          ...memoryProvider,
          api_key: '',
          thinking_budget: memoryProvider.thinking_budget ?? 0,
          reasoning_effort: memoryProvider.reasoning_effort ?? '',
          gemini_thinking_mode: memoryProvider.gemini_thinking_mode || 'none',
          gemini_thinking_level: memoryProvider.gemini_thinking_level || 'low',
          gemini_thinking_budget: memoryProvider.gemini_thinking_budget || 128,
          temperature: memoryProvider.type === 'anthropic' ? 1 : memoryProvider.temperature,
        }
      : { ...emptyProvider }

    setEditingProvider(base)
    setShowModal(true)
  }

  const handleCloseModal = () => {
    if (modalRef.current) {
      gsap.to(modalRef.current, {
        opacity: 0,
        scale: 0.9,
        duration: 0.2,
        ease: 'power2.in',
        onComplete: () => {
          setShowModal(false)
          setEditingProvider(null)
        },
      })
    } else {
      setShowModal(false)
      setEditingProvider(null)
    }
  }

  const handleProviderChange = (field: keyof Provider, value: string | boolean | number) => {
    if (!editingProvider) return

    if (field === 'model') {
      const nextModel = String(value || '')
      const nextProvider: Provider = { ...editingProvider, model: nextModel }
      if (nextProvider.type === 'gemini' && nextProvider.gemini_thinking_mode === 'thinking_budget') {
        const nextBudget = Number(nextProvider.gemini_thinking_budget) || 0
        nextProvider.gemini_thinking_budget = clampGeminiThinkingBudget(nextModel, nextBudget)
      }
      setEditingProvider(nextProvider)
      return
    }

    if (field === 'type') {
      const nextType = value as ProviderType
      const nextProvider: Provider = {
        ...editingProvider,
        type: nextType,
        temperature: nextType === 'anthropic' ? 1 : editingProvider.temperature,
      }
      setEditingProvider(nextProvider)
      return
    }

    if (field === 'gemini_thinking_mode') {
      const nextMode = value as string
      const nextProvider: Provider = {
        ...editingProvider,
        gemini_thinking_mode: nextMode,
      }
      if (nextMode === 'thinking_level' && !nextProvider.gemini_thinking_level) {
        nextProvider.gemini_thinking_level = 'low'
      }
      if (nextMode === 'thinking_budget') {
        const nextBudget = Number(nextProvider.gemini_thinking_budget) || getGeminiThinkingBudgetRange(nextProvider.model).min
        nextProvider.gemini_thinking_budget = clampGeminiThinkingBudget(nextProvider.model, nextBudget)
      }
      setEditingProvider(nextProvider)
      return
    }

    if (field === 'gemini_thinking_budget') {
      const nextBudget = Number(value) || 0
      setEditingProvider({
        ...editingProvider,
        gemini_thinking_budget: clampGeminiThinkingBudget(editingProvider.model, nextBudget),
      })
      return
    }

    setEditingProvider({ ...editingProvider, [field]: value })
  }

  const handleSaveProvider = async () => {
    if (!editingProvider) return

    if (!editingProvider.id || !editingProvider.name) {
      showMessageToast('ID 和名称为必填项')
      return
    }

    if (editingProvider.type === 'gemini_image') {
      showMessageToast('Gemini 生图不允许用于记忆')
      return
    }

    setSaving(true)
    const updated = await updateMemoryProvider(true, editingProvider)
    if (updated) {
      setMemoryProvider(updated)
      showMessageToast('记忆提供商已保存')
      handleCloseModal()
    } else {
      showMessageToast('保存失败')
    }
    setSaving(false)
  }

  const isFollowChat = memoryProvider == null

  return (
    <div className="provider-settings" ref={containerRef}>
      <div className="provider-settings-header">
        <button className="back-button" onClick={handleBack}>
          <svg viewBox="0 0 24 24">
            <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
          </svg>
        </button>
        <div className="provider-settings-title">记忆提供商</div>
        <div style={{ width: 44 }}></div>
      </div>

      {loading ? (
        <div className="provider-settings-loading">加载中...</div>
      ) : (
        <div className="provider-settings-content">
          <div style={{ marginBottom: 12, color: 'var(--text-secondary)', fontSize: 12, lineHeight: 1.4 }}>
            提示：记忆提供商仅用于长期记忆提取，不影响对话模型。Gemini 生图不允许作为记忆提供商。
          </div>

          <div className="provider-cards">
            <div
              className={`provider-card ${isFollowChat ? 'active' : 'inactive'}`}
              onClick={() => {
                if (isFollowChat) return
                handleUseFollowChat()
              }}
            >
              <div className="provider-card-header">
                <div className="provider-card-id">跟随对话模型</div>
                {isFollowChat && <div className="active-indicator">当前</div>}
              </div>
              <div className="provider-card-body">
                <div className="provider-card-row">
                  <span className="provider-card-label">名称</span>
                  <span className="provider-card-value">{activeChatProvider?.name || '未设置'}</span>
                </div>
                <div className="provider-card-row">
                  <span className="provider-card-label">模型</span>
                  <span className="provider-card-value model">{activeChatProvider?.model || '未设置'}</span>
                </div>
                <div className="provider-card-row">
                  <span className="provider-card-label">类型</span>
                  <span className="provider-card-value type">{activeChatProvider?.type || 'unknown'}</span>
                </div>
              </div>
            </div>

            <div
              className={`provider-card ${!isFollowChat ? 'active' : 'inactive'}`}
              onClick={() => openEditModal()}
            >
              <div className="provider-card-header">
                <div className="provider-card-id">独立记忆提供商</div>
                {!isFollowChat && <div className="active-indicator">当前</div>}
              </div>
              <div className="provider-card-body">
                <div className="provider-card-row">
                  <span className="provider-card-label">名称</span>
                  <span className="provider-card-value">{memoryProvider?.name || '未配置'}</span>
                </div>
                <div className="provider-card-row">
                  <span className="provider-card-label">模型</span>
                  <span className="provider-card-value model">{memoryProvider?.model || '未配置'}</span>
                </div>
                <div className="provider-card-row">
                  <span className="provider-card-label">类型</span>
                  <span className="provider-card-value type">{memoryProvider?.type || 'unknown'}</span>
                </div>
              </div>
              <div className="provider-card-actions">
                <button
                  className="card-action-btn edit"
                  onClick={(e) => {
                    e.stopPropagation()
                    openEditModal()
                  }}
                >
                  <svg viewBox="0 0 24 24">
                    <path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04a1.003 1.003 0 000-1.42l-2.34-2.34a1.003 1.003 0 00-1.42 0l-1.83 1.83 3.75 3.75 1.84-1.82z" />
                  </svg>
                  {memoryProvider ? '编辑' : '配置'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {showModal && editingProvider && (
        <div className="modal-overlay" onClick={handleCloseModal}>
          <div className="modal-card" ref={modalRef} onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>记忆提供商配置</h3>
              <button className="modal-close" onClick={handleCloseModal}>
                <svg viewBox="0 0 24 24">
                  <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                </svg>
              </button>
            </div>

            <div className="modal-body">
              <div className="modal-group">
                <label className="modal-label">供应商ID</label>
                <input
                  type="text"
                  className="modal-input"
                  value={editingProvider.id}
                  onChange={(e) => handleProviderChange('id', e.target.value)}
                  placeholder="memory"
                />
              </div>

              <div className="modal-group">
                <label className="modal-label">显示名称</label>
                <input
                  type="text"
                  className="modal-input"
                  value={editingProvider.name}
                  onChange={(e) => handleProviderChange('name', e.target.value)}
                  placeholder="记忆提供商"
                />
              </div>

              <div className="modal-group">
                <label className="modal-label">供应商类型</label>
                <select
                  className="modal-input modal-select"
                  value={editingProvider.type || 'openai'}
                  onChange={(e) => handleProviderChange('type', e.target.value)}
                >
                  {PROVIDER_TYPES.map((type) => (
                    <option key={type.value} value={type.value}>
                      {type.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className="modal-group">
                <label className="modal-label">API 地址 (URL)</label>
                <input
                  type="text"
                  className="modal-input"
                  value={editingProvider.base_url}
                  onChange={(e) => handleProviderChange('base_url', e.target.value)}
                  placeholder="https://api.openai.com/v1"
                />
              </div>

              <div className="modal-group">
                <label className="modal-label">API 密钥 (Key)</label>
                <input
                  type="password"
                  className="modal-input"
                  value={editingProvider.api_key}
                  onChange={(e) => handleProviderChange('api_key', e.target.value)}
                  placeholder={memoryProvider ? '留空保持不变' : 'sk-...'}
                />
              </div>

              <div className="modal-group">
                <label className="modal-label">模型</label>
                <input
                  type="text"
                  className="modal-input"
                  value={editingProvider.model}
                  onChange={(e) => handleProviderChange('model', e.target.value)}
                  placeholder="gpt-4"
                />
              </div>

              <div className="modal-group">
                <label className="modal-label">温度 (0-2)</label>
                <input
                  type="number"
                  className="modal-input"
                  min={0}
                  max={2}
                  step={0.1}
                  value={editingProvider.temperature}
                  onChange={(e) => handleProviderChange('temperature', Number(e.target.value) || 0)}
                  placeholder="0.8"
                  disabled={editingProvider.type === 'anthropic'}
                />
              </div>

              <div className="modal-group">
                <label className="modal-label">Top P (0-1)</label>
                <input
                  type="number"
                  className="modal-input"
                  min={0}
                  max={1}
                  step={0.1}
                  value={editingProvider.top_p}
                  onChange={(e) => handleProviderChange('top_p', Number(e.target.value) || 0)}
                  placeholder="1"
                />
              </div>

              {(editingProvider.type === 'openai' || editingProvider.type === 'openai_response') && (
                <div className="modal-group">
                  <label className="modal-label">思考量 (reasoning effort)</label>
                  <select
                    className="modal-input modal-select"
                    value={editingProvider.reasoning_effort}
                    onChange={(e) => handleProviderChange('reasoning_effort', e.target.value)}
                  >
                    {OPENAI_REASONING_EFFORT_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </div>
              )}

              {editingProvider.type === 'gemini' && (
                <>
                  <div className="modal-group">
                    <label className="modal-label">Gemini 思考模式</label>
                    <select
                      className="modal-input modal-select"
                      value={editingProvider.gemini_thinking_mode || 'none'}
                      onChange={(e) => handleProviderChange('gemini_thinking_mode', e.target.value)}
                    >
                      {GEMINI_THINKING_MODES.map((mode) => (
                        <option key={mode.value} value={mode.value}>
                          {mode.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div className="modal-group">
                    <label className="modal-label">Gemini 思考参数</label>
                    {editingProvider.gemini_thinking_mode === 'thinking_level' && (
                      <select
                        className="modal-input modal-select"
                        value={editingProvider.gemini_thinking_level || 'low'}
                        onChange={(e) => handleProviderChange('gemini_thinking_level', e.target.value)}
                      >
                        {GEMINI_THINKING_LEVELS.map((level) => (
                          <option key={level.value} value={level.value}>
                            {level.label}
                          </option>
                        ))}
                      </select>
                    )}
                    {editingProvider.gemini_thinking_mode === 'thinking_budget' && (
                      <input
                        type="number"
                        className="modal-input"
                        min={getGeminiThinkingBudgetRange(editingProvider.model).min}
                        max={getGeminiThinkingBudgetRange(editingProvider.model).max}
                        step={1}
                        value={editingProvider.gemini_thinking_budget}
                        onChange={(e) => handleProviderChange('gemini_thinking_budget', Number(e.target.value) || 0)}
                        placeholder={`${getGeminiThinkingBudgetRange(editingProvider.model).min}-${getGeminiThinkingBudgetRange(editingProvider.model).max}`}
                      />
                    )}
                    {editingProvider.gemini_thinking_mode === 'none' && (
                      <input
                        type="text"
                        className="modal-input"
                        value="已关闭"
                        disabled
                      />
                    )}
                  </div>
                </>
              )}

              {editingProvider.type === 'anthropic' && (
                <div className="modal-group">
                  <label className="modal-label">思考预算 (tokens)</label>
                  <input
                    type="number"
                    className="modal-input"
                    min={0}
                    step={1}
                    value={editingProvider.thinking_budget}
                    onChange={(e) => handleProviderChange('thinking_budget', Number(e.target.value) || 0)}
                    placeholder="0"
                  />
                </div>
              )}

              {/* 记忆提取不需要上下文轮数、流式输出和识图能力配置 */}
            </div>

            <div className="modal-footer">
              <button className="modal-btn cancel" onClick={handleCloseModal}>
                取消
              </button>
              <button
                className="modal-btn save"
                onClick={handleSaveProvider}
                disabled={saving}
              >
                {saving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      {message && (
        <div className={`provider-message ${message.includes('成功') || message.includes('已') ? 'success' : 'error'}`}>
          {message}
        </div>
      )}
    </div>
  )
}

export default MemoryProviderSettings
