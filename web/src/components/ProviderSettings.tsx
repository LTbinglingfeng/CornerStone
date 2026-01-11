import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import type { Provider, ProviderType } from '../types/chat'
import {
  getProviders,
  addProvider,
  updateProvider,
  deleteProvider,
  setActiveProvider,
} from '../services/api'
import './ProviderSettings.css'

// 供应商类型选项
const PROVIDER_TYPES: { value: ProviderType; label: string }[] = [
  { value: 'openai', label: 'OpenAI 兼容' },
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

interface ProviderSettingsProps {
  onBack: () => void
}

const ProviderSettings: React.FC<ProviderSettingsProps> = ({ onBack }) => {
  const [providers, setProviders] = useState<Provider[]>([])
  const [activeProviderId, setActiveProviderId] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState('')
  const [showModal, setShowModal] = useState(false)
  const [editingProvider, setEditingProvider] = useState<Provider | null>(null)
  const [isAddingNew, setIsAddingNew] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const modalRef = useRef<HTMLDivElement>(null)

  const emptyProvider: Provider = {
    id: '',
    name: '',
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
    loadProviders()
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

  const loadProviders = async () => {
    setLoading(true)
    const data = await getProviders()
    if (data) {
      setProviders(data.providers)
      setActiveProviderId(data.active_provider_id)
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

  const showMessage = (msg: string) => {
    setMessage(msg)
    setTimeout(() => setMessage(''), 2000)
  }

  const handleSetActive = async (providerId: string) => {
    const success = await setActiveProvider(providerId)
    if (success) {
      setActiveProviderId(providerId)
      showMessage('已切换供应商')
    } else {
      showMessage('切换失败')
    }
  }

  const handleAddNew = () => {
    setEditingProvider({ ...emptyProvider })
    setIsAddingNew(true)
    setShowModal(true)
  }

  const handleEditProvider = (provider: Provider) => {
    setEditingProvider({
      ...provider,
      api_key: '',
      thinking_budget: provider.thinking_budget ?? 0,
      reasoning_effort: provider.reasoning_effort ?? '',
      gemini_thinking_mode: provider.gemini_thinking_mode || 'none',
      gemini_thinking_level: provider.gemini_thinking_level || 'low',
      gemini_thinking_budget: provider.gemini_thinking_budget || 128,
      temperature: provider.type === 'anthropic' ? 1 : provider.temperature,
    })
    setIsAddingNew(false)
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
          setIsAddingNew(false)
        },
      })
    } else {
      setShowModal(false)
      setEditingProvider(null)
      setIsAddingNew(false)
    }
  }

  const handleSaveProvider = async () => {
    if (!editingProvider) return

    if (!editingProvider.id || !editingProvider.name) {
      showMessage('ID 和名称为必填项')
      return
    }

    setSaving(true)

    if (isAddingNew) {
      const result = await addProvider(editingProvider)
      if (result) {
        await loadProviders()
        showMessage('供应商添加成功')
        handleCloseModal()
      } else {
        showMessage('添加失败，ID 可能已存在')
      }
    } else {
      const success = await updateProvider(editingProvider)
      if (success) {
        await loadProviders()
        showMessage('供应商更新成功')
        handleCloseModal()
      } else {
        showMessage('更新失败')
      }
    }

    setSaving(false)
  }

  const handleDeleteProvider = async (id: string) => {
    if (providers.length <= 1) {
      showMessage('至少保留一个供应商')
      return
    }

    if (!confirm('确定要删除此供应商吗？')) return

    const success = await deleteProvider(id)
    if (success) {
      await loadProviders()
      showMessage('供应商已删除')
    } else {
      showMessage('删除失败')
    }
  }

  const handleProviderChange = (field: keyof Provider, value: string | boolean | number) => {
    if (!editingProvider) return
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
        const nextBudget = Number(nextProvider.gemini_thinking_budget) || 128
        nextProvider.gemini_thinking_budget = Math.min(Math.max(nextBudget, 128), 32768)
      }
      setEditingProvider(nextProvider)
      return
    }
    setEditingProvider({ ...editingProvider, [field]: value })
  }

  const maskApiKey = (key: string): string => {
    if (!key || key.length <= 8) return '••••••••'
    return key.substring(0, 4) + '••••••••' + key.substring(key.length - 4)
  }

  return (
    <div className="provider-settings" ref={containerRef}>
      <div className="provider-settings-header">
        <button className="back-button" onClick={handleBack}>
          <svg viewBox="0 0 24 24">
            <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
          </svg>
        </button>
        <div className="provider-settings-title">供应商管理</div>
        <button className="header-add-button" onClick={handleAddNew}>
          <svg viewBox="0 0 24 24">
            <path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z" />
          </svg>
        </button>
      </div>

      {loading ? (
        <div className="provider-settings-loading">加载中...</div>
      ) : (
        <div className="provider-settings-content">
          <div className="provider-cards">
            {providers.map((provider) => {
              const isActive = provider.id === activeProviderId
              return (
                <div
                  key={provider.id}
                  className={`provider-card ${isActive ? 'active' : 'inactive'}`}
                  onClick={() => !isActive && handleSetActive(provider.id)}
                >
                  <div className="provider-card-header">
                    <div className="provider-card-id">{provider.id}</div>
                    {isActive && <span className="active-indicator">使用中</span>}
                  </div>
                  <div className="provider-card-body">
                    <div className="provider-card-row">
                      <span className="provider-card-label">类型</span>
                      <span className="provider-card-value type">
                        {PROVIDER_TYPES.find(t => t.value === provider.type)?.label || provider.type || 'OpenAI 兼容'}
                      </span>
                    </div>
                    <div className="provider-card-row">
                      <span className="provider-card-label">URL</span>
                      <span className="provider-card-value">{provider.base_url}</span>
                    </div>
                    <div className="provider-card-row">
                      <span className="provider-card-label">Key</span>
                      <span className="provider-card-value masked">{maskApiKey(provider.api_key)}</span>
                    </div>
                    <div className="provider-card-row">
                      <span className="provider-card-label">模型</span>
                      <span className="provider-card-value model">{provider.model}</span>
                    </div>
                    <div className="provider-card-row">
                      <span className="provider-card-label">温度</span>
                      <span className="provider-card-value">{provider.temperature}</span>
                    </div>
                    <div className="provider-card-row">
                      <span className="provider-card-label">Top P</span>
                      <span className="provider-card-value">{provider.top_p}</span>
                    </div>
                    <div className="provider-card-row">
                      <span className="provider-card-label">上下文</span>
                      <span className="provider-card-value">{provider.context_messages} 轮</span>
                    </div>
                    <div className="provider-card-row">
                      <span className="provider-card-label">流式</span>
                      <span className={`provider-card-value ${provider.stream ? 'stream-on' : 'stream-off'}`}>
                        {provider.stream ? '开启' : '关闭'}
                      </span>
                    </div>
                    <div className="provider-card-row">
                      <span className="provider-card-label">识图</span>
                      <span className={`provider-card-value ${provider.image_capable ? 'vision-on' : 'vision-off'}`}>
                        {provider.image_capable ? '支持' : '不支持'}
                      </span>
                    </div>
                  </div>
                  <div className="provider-card-actions">
                    <button
                      className="card-action-btn edit"
                      onClick={(e) => {
                        e.stopPropagation()
                        handleEditProvider(provider)
                      }}
                    >
                      <svg viewBox="0 0 24 24">
                        <path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34c-.39-.39-1.02-.39-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z" />
                      </svg>
                      编辑
                    </button>
                    <button
                      className="card-action-btn delete"
                      onClick={(e) => {
                        e.stopPropagation()
                        handleDeleteProvider(provider.id)
                      }}
                    >
                      <svg viewBox="0 0 24 24">
                        <path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z" />
                      </svg>
                      删除
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* 悬浮编辑/新增卡片 */}
      {showModal && editingProvider && (
        <div className="modal-overlay" onClick={handleCloseModal}>
          <div
            className="modal-card"
            ref={modalRef}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="modal-header">
              <h3>{isAddingNew ? '添加供应商' : '编辑供应商'}</h3>
              <button className="modal-close" onClick={handleCloseModal}>
                <svg viewBox="0 0 24 24">
                  <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                </svg>
              </button>
            </div>

            <div className="modal-body">
              <div className="modal-group">
                <label className="modal-label">供应商 ID</label>
                <input
                  type="text"
                  className="modal-input"
                  value={editingProvider.id}
                  onChange={(e) => handleProviderChange('id', e.target.value)}
                  placeholder="unique-id"
                  disabled={!isAddingNew}
                />
              </div>

              <div className="modal-group">
                <label className="modal-label">显示名称</label>
                <input
                  type="text"
                  className="modal-input"
                  value={editingProvider.name}
                  onChange={(e) => handleProviderChange('name', e.target.value)}
                  placeholder="OpenAI"
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
                  placeholder={isAddingNew ? 'sk-...' : '留空保持不变'}
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

              {editingProvider.type === 'openai' && (
                <div className="modal-group">
                  <label className="modal-label">思考强度 (reasoning_effort)</label>
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
                      value={editingProvider.gemini_thinking_mode}
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
                    <label className="modal-label">思考级别 / 预算</label>
                    {editingProvider.gemini_thinking_mode === 'thinking_level' && (
                      <select
                        className="modal-input modal-select"
                        value={editingProvider.gemini_thinking_level}
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
                        min={128}
                        max={32768}
                        step={1}
                        value={editingProvider.gemini_thinking_budget}
                        onChange={(e) => handleProviderChange('gemini_thinking_budget', Number(e.target.value) || 0)}
                        placeholder="128-32768"
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

              <div className="modal-group">
                <label className="modal-label">上下文轮数</label>
                <input
                  type="number"
                  className="modal-input"
                  min={1}
                  step={1}
                  value={editingProvider.context_messages}
                  onChange={(e) => handleProviderChange('context_messages', Number(e.target.value) || 0)}
                  placeholder="64"
                />
              </div>

              <div className="modal-group">
                <label className="modal-label">流式输出</label>
                <div className="modal-toggle-wrapper">
                  <label className="toggle-switch">
                    <input
                      type="checkbox"
                      checked={editingProvider.stream}
                      onChange={(e) => handleProviderChange('stream', e.target.checked)}
                    />
                    <span className="toggle-slider"></span>
                  </label>
                  <span className="toggle-label">{editingProvider.stream ? '开启' : '关闭'}</span>
                </div>
              </div>

              <div className="modal-group">
                <label className="modal-label">支持识图</label>
                <div className="modal-toggle-wrapper">
                  <label className="toggle-switch">
                    <input
                      type="checkbox"
                      checked={editingProvider.image_capable}
                      onChange={(e) => handleProviderChange('image_capable', e.target.checked)}
                    />
                    <span className="toggle-slider"></span>
                  </label>
                  <span className="toggle-label">{editingProvider.image_capable ? '支持' : '不支持'}</span>
                </div>
              </div>
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

export default ProviderSettings
