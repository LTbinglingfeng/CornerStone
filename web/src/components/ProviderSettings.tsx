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
import {
  PROVIDER_TYPES_ALL,
  OPENAI_REASONING_EFFORT_OPTIONS,
  GEMINI_THINKING_MODES,
  GEMINI_THINKING_LEVELS,
  GEMINI_IMAGE_ASPECT_RATIOS,
  GEMINI_IMAGE_SIZES,
  GEMINI_IMAGE_OUTPUT_MIME_TYPES,
  getGeminiThinkingBudgetRange,
  clampGeminiThinkingBudget,
  clampGeminiImageNumberOfImages,
  maskApiKey,
  CustomSelect,
} from './provider'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import './ProviderSettings.css'

interface ProviderSettingsProps {
  onBack: () => void
}

const ProviderSettings: React.FC<ProviderSettingsProps> = ({ onBack }) => {
  const { showToast } = useToast()
  const { confirm } = useConfirm()
  const [providers, setProviders] = useState<Provider[]>([])
  const [activeProviderId, setActiveProviderId] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
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
    gemini_image_aspect_ratio: '1:1',
    gemini_image_size: '',
    gemini_image_number_of_images: 1,
    gemini_image_output_mime_type: 'image/jpeg',
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

  const handleSetActive = async (providerId: string) => {
    const success = await setActiveProvider(providerId)
    if (success) {
      setActiveProviderId(providerId)
      showToast('已切换供应商', 'success')
    } else {
      showToast('切换失败', 'error')
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
      gemini_image_aspect_ratio: provider.gemini_image_aspect_ratio || '1:1',
      gemini_image_size: provider.gemini_image_size || '',
      gemini_image_number_of_images: provider.gemini_image_number_of_images ?? 1,
      gemini_image_output_mime_type: provider.gemini_image_output_mime_type || 'image/jpeg',
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
      showToast('ID 和名称为必填项', 'error')
      return
    }

    setSaving(true)

    if (isAddingNew) {
      const result = await addProvider(editingProvider)
      if (result) {
        await loadProviders()
        showToast('供应商添加成功', 'success')
        handleCloseModal()
      } else {
        showToast('添加失败，ID 可能已存在', 'error')
      }
    } else {
      const success = await updateProvider(editingProvider)
      if (success) {
        await loadProviders()
        showToast('供应商更新成功', 'success')
        handleCloseModal()
      } else {
        showToast('更新失败', 'error')
      }
    }

    setSaving(false)
  }

  const handleDeleteProvider = async (id: string) => {
    if (providers.length <= 1) {
      showToast('至少保留一个供应商', 'error')
      return
    }

    const ok = await confirm({
      title: '删除供应商',
      message: '确定要删除此供应商吗？',
      confirmText: '删除',
      danger: true
    })
    if (!ok) return

    const success = await deleteProvider(id)
    if (success) {
      await loadProviders()
      showToast('供应商已删除', 'success')
    } else {
      showToast('删除失败', 'error')
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
      if (nextType === 'gemini_image') {
        nextProvider.gemini_image_aspect_ratio = nextProvider.gemini_image_aspect_ratio || '1:1'
        nextProvider.gemini_image_size = nextProvider.gemini_image_size || ''
        nextProvider.gemini_image_number_of_images = clampGeminiImageNumberOfImages(nextProvider.gemini_image_number_of_images ?? 1)
        nextProvider.gemini_image_output_mime_type = nextProvider.gemini_image_output_mime_type || 'image/jpeg'
      } else {
        nextProvider.gemini_image_aspect_ratio = undefined
        nextProvider.gemini_image_size = undefined
        nextProvider.gemini_image_number_of_images = undefined
        nextProvider.gemini_image_output_mime_type = undefined
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

    if (field === 'gemini_image_number_of_images') {
      const nextNumber = clampGeminiImageNumberOfImages(Number(value) || 0)
      setEditingProvider({
        ...editingProvider,
        gemini_image_number_of_images: nextNumber,
      })
      return
    }

    setEditingProvider({ ...editingProvider, [field]: value })
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
              const isChatSelectable = provider.type !== 'gemini_image'
              return (
                <div
                  key={provider.id}
                  className={`provider-card ${isActive ? 'active' : 'inactive'}`}
                  onClick={() => {
                    if (isActive) return
                    if (!isChatSelectable) {
                      showToast('该供应商仅用于生图，无法用于对话', 'info')
                      return
                    }
                    handleSetActive(provider.id)
                  }}
                >
                  <div className="provider-card-header">
                    <div className="provider-card-id">{provider.id}</div>
                    {isActive && <span className="active-indicator">使用中</span>}
                  </div>
                  <div className="provider-card-body">
                    <div className="provider-card-row">
                      <span className="provider-card-label">类型</span>
                      <span className="provider-card-value type">
                        {PROVIDER_TYPES_ALL.find(t => t.value === provider.type)?.label || provider.type || 'OpenAI 兼容'}
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
                <CustomSelect
                  value={editingProvider.type || 'openai'}
                  options={PROVIDER_TYPES_ALL}
                  ariaLabel="供应商类型"
                  onChange={(value) => handleProviderChange('type', value)}
                />
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
                  placeholder={editingProvider.type === 'gemini_image' ? 'nano banana / nanobanana Pro' : 'gpt-4'}
                />
              </div>

              {editingProvider.type === 'gemini_image' && (
                <>
                  <div className="modal-group">
                    <label className="modal-label">生图比例</label>
                    <select
                      className="modal-input modal-select"
                      value={editingProvider.gemini_image_aspect_ratio || '1:1'}
                      onChange={(e) => handleProviderChange('gemini_image_aspect_ratio', e.target.value)}
                    >
                      {GEMINI_IMAGE_ASPECT_RATIOS.map((ratio) => (
                        <option key={ratio.value} value={ratio.value}>
                          {ratio.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div className="modal-group">
                    <label className="modal-label">生图分辨率 (最大边)</label>
                    <select
                      className="modal-input modal-select"
                      value={editingProvider.gemini_image_size || ''}
                      onChange={(e) => handleProviderChange('gemini_image_size', e.target.value)}
                    >
                      {GEMINI_IMAGE_SIZES.map((size) => (
                        <option key={size.value || 'default'} value={size.value}>
                          {size.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div className="modal-group">
                    <label className="modal-label">生图数量 (1-8)</label>
                    <input
                      type="number"
                      className="modal-input"
                      min={1}
                      max={8}
                      step={1}
                      value={editingProvider.gemini_image_number_of_images ?? 1}
                      onChange={(e) => handleProviderChange('gemini_image_number_of_images', Number(e.target.value) || 0)}
                      placeholder="1"
                    />
                  </div>

                  <div className="modal-group">
                    <label className="modal-label">输出格式</label>
                    <select
                      className="modal-input modal-select"
                      value={editingProvider.gemini_image_output_mime_type || 'image/jpeg'}
                      onChange={(e) => handleProviderChange('gemini_image_output_mime_type', e.target.value)}
                    >
                      {GEMINI_IMAGE_OUTPUT_MIME_TYPES.map((mime) => (
                        <option key={mime.value} value={mime.value}>
                          {mime.label}
                        </option>
                      ))}
                    </select>
                  </div>
                </>
              )}

              {editingProvider.type !== 'gemini_image' && (
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
              )}

              {editingProvider.type !== 'gemini_image' && (
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
              )}

              {(editingProvider.type === 'openai' || editingProvider.type === 'openai_response') && (
                <div className="modal-group">
                  <label className="modal-label">思考量 (reasoning effort)</label>
                  <CustomSelect
                    value={editingProvider.reasoning_effort ?? ''}
                    options={OPENAI_REASONING_EFFORT_OPTIONS}
                    ariaLabel="思考量"
                    onChange={(value) => handleProviderChange('reasoning_effort', value)}
                  />
                </div>
              )}

              {editingProvider.type === 'gemini' && (
                <>
                  <div className="modal-group">
                    <label className="modal-label">Gemini 思考模式</label>
                    <CustomSelect
                      value={editingProvider.gemini_thinking_mode || 'none'}
                      options={GEMINI_THINKING_MODES}
                      ariaLabel="Gemini 思考模式"
                      onChange={(value) => handleProviderChange('gemini_thinking_mode', value)}
                    />
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

              {editingProvider.type !== 'gemini_image' && (
                <>
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
                </>
              )}
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
    </div>
  )
}

export default ProviderSettings
