import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import { getProviders, updateSystemPrompt } from '../services/api'
import ProviderSettings from './ProviderSettings'
import './Settings.css'

interface SettingsProps {
  onBack: () => void
}

const Settings: React.FC<SettingsProps> = ({ onBack }) => {
  const [systemPrompt, setSystemPrompt] = useState('')
  const [editingPrompt, setEditingPrompt] = useState('')
  const [activeProviderName, setActiveProviderName] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState('')
  const [showProviderSettings, setShowProviderSettings] = useState(false)
  const [showPromptModal, setShowPromptModal] = useState(false)
  const promptModalRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    loadData()
  }, [])

  useEffect(() => {
    if (showPromptModal && promptModalRef.current) {
      gsap.fromTo(
        promptModalRef.current,
        { opacity: 0, scale: 0.9 },
        { opacity: 1, scale: 1, duration: 0.2, ease: 'power2.out' }
      )
    }
  }, [showPromptModal])

  const loadData = async () => {
    setLoading(true)
    const providersData = await getProviders()
    if (providersData) {
      setSystemPrompt(providersData.system_prompt)
      const activeProvider = providersData.providers.find(p => p.id === providersData.active_provider_id)
      setActiveProviderName(activeProvider?.name || '未设置')
    }
    setLoading(false)
  }

  const showMessage = (msg: string) => {
    setMessage(msg)
    setTimeout(() => setMessage(''), 2000)
  }

  const handleOpenPromptModal = () => {
    setEditingPrompt(systemPrompt)
    setShowPromptModal(true)
  }

  const handleClosePromptModal = () => {
    if (promptModalRef.current) {
      gsap.to(promptModalRef.current, {
        opacity: 0,
        scale: 0.9,
        duration: 0.2,
        ease: 'power2.in',
        onComplete: () => {
          setShowPromptModal(false)
        },
      })
    } else {
      setShowPromptModal(false)
    }
  }

  const handleSaveSystemPrompt = async () => {
    setSaving(true)
    const success = await updateSystemPrompt(editingPrompt)
    if (success) {
      setSystemPrompt(editingPrompt)
      showMessage('系统提示词已保存')
      handleClosePromptModal()
    } else {
      showMessage('保存失败')
    }
    setSaving(false)
  }

  const handleProviderSettingsBack = () => {
    setShowProviderSettings(false)
    loadData()
  }

  const getPromptPreview = () => {
    if (!systemPrompt) return '未设置'
    if (systemPrompt.length <= 20) return systemPrompt
    return systemPrompt.substring(0, 20) + '...'
  }

  return (
    <div className="settings">
      <div className="settings-header">
        <button className="back-button" onClick={onBack}>
          <svg viewBox="0 0 24 24">
            <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
          </svg>
        </button>
        <div className="settings-title">设置</div>
        <div style={{ width: 44 }}></div>
      </div>

      {loading ? (
        <div className="settings-loading">加载中...</div>
      ) : (
        <div className="settings-content">
          {/* 供应商设置入口 */}
          <div className="settings-section">
            <h3>供应商</h3>
            <button
              className="settings-entry-btn"
              onClick={() => setShowProviderSettings(true)}
            >
              <div className="settings-entry-info">
                <span className="settings-entry-label">当前供应商</span>
                <span className="settings-entry-value">{activeProviderName}</span>
              </div>
              <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
              </svg>
            </button>
          </div>

          {/* 全局设置 */}
          <div className="settings-section">
            <h3>全局设置</h3>
            <button
              className="settings-entry-btn"
              onClick={handleOpenPromptModal}
            >
              <div className="settings-entry-info">
                <span className="settings-entry-label">默认系统提示词</span>
                <span className="settings-entry-value">{getPromptPreview()}</span>
              </div>
              <svg className="settings-entry-arrow" viewBox="0 0 24 24">
                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
              </svg>
            </button>
          </div>

          {message && (
            <div className={`settings-message ${message.includes('成功') || message.includes('已') ? 'success' : 'error'}`}>
              {message}
            </div>
          )}
        </div>
      )}

      {/* 供应商管理二级界面 */}
      {showProviderSettings && (
        <ProviderSettings onBack={handleProviderSettingsBack} />
      )}

      {/* 系统提示词编辑弹窗 */}
      {showPromptModal && (
        <div className="prompt-modal-overlay" onClick={handleClosePromptModal}>
          <div
            className="prompt-modal-card"
            ref={promptModalRef}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="prompt-modal-header">
              <h3>编辑系统提示词</h3>
              <button className="prompt-modal-close" onClick={handleClosePromptModal}>
                <svg viewBox="0 0 24 24">
                  <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                </svg>
              </button>
            </div>

            <div className="prompt-modal-body">
              <p className="prompt-modal-hint">此提示词将作为所有对话的默认全局系统提示词</p>
              <textarea
                className="prompt-modal-textarea"
                value={editingPrompt}
                onChange={(e) => setEditingPrompt(e.target.value)}
                placeholder="输入系统提示词..."
                rows={8}
              />
            </div>

            <div className="prompt-modal-footer">
              <button className="prompt-modal-btn cancel" onClick={handleClosePromptModal}>
                取消
              </button>
              <button
                className="prompt-modal-btn save"
                onClick={handleSaveSystemPrompt}
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

export default Settings
