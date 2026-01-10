import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import { getUserInfo, updateUserInfo, uploadUserAvatar, getUserAvatarUrl } from '../services/api'
import type { UserInfo } from '../types/chat'
import Settings from './Settings'
import './ProfilePage.css'

const ProfilePage: React.FC = () => {
  const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [showSettings, setShowSettings] = useState(false)
  const [showUserModal, setShowUserModal] = useState(false)
  const [editingUserInfo, setEditingUserInfo] = useState({ username: '', description: '' })
  const [userAvatarFile, setUserAvatarFile] = useState<File | null>(null)
  const [userAvatarPreview, setUserAvatarPreview] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState('')
  const userModalRef = useRef<HTMLDivElement>(null)
  const userFileInputRef = useRef<HTMLInputElement>(null)
  const settingsRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    loadUserInfo()
  }, [])

  useEffect(() => {
    if (showUserModal && userModalRef.current) {
      gsap.fromTo(
        userModalRef.current,
        { opacity: 0, scale: 0.9 },
        { opacity: 1, scale: 1, duration: 0.2, ease: 'power2.out' }
      )
    }
  }, [showUserModal])

  useEffect(() => {
    if (showSettings && settingsRef.current) {
      gsap.set(settingsRef.current, { x: '100%' })
      gsap.to(settingsRef.current, {
        x: 0,
        duration: 0.3,
        ease: 'power2.out'
      })
    }
  }, [showSettings])

  const loadUserInfo = async () => {
    setLoading(true)
    const userData = await getUserInfo()
    if (userData) {
      setUserInfo(userData)
    }
    setLoading(false)
  }

  const showMessageToast = (msg: string) => {
    setMessage(msg)
    setTimeout(() => setMessage(''), 2000)
  }

  const handleOpenUserModal = () => {
    setEditingUserInfo({
      username: userInfo?.username || '',
      description: userInfo?.description || ''
    })
    if (userInfo?.avatar) {
      setUserAvatarPreview(getUserAvatarUrl() + `?t=${Date.now()}`)
    } else {
      setUserAvatarPreview(null)
    }
    setUserAvatarFile(null)
    setShowUserModal(true)
  }

  const handleCloseUserModal = () => {
    if (userModalRef.current) {
      gsap.to(userModalRef.current, {
        opacity: 0,
        scale: 0.9,
        duration: 0.2,
        ease: 'power2.in',
        onComplete: () => {
          setShowUserModal(false)
        },
      })
    } else {
      setShowUserModal(false)
    }
  }

  const handleUserAvatarClick = () => {
    userFileInputRef.current?.click()
  }

  const handleUserFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) {
      setUserAvatarFile(file)
      const reader = new FileReader()
      reader.onloadend = () => {
        setUserAvatarPreview(reader.result as string)
      }
      reader.readAsDataURL(file)
    }
  }

  const handleSaveUserInfo = async () => {
    setSaving(true)
    try {
      const updated = await updateUserInfo(editingUserInfo)
      if (updated) {
        if (userAvatarFile) {
          await uploadUserAvatar(userAvatarFile)
        }
        setUserInfo(updated)
        showMessageToast('个人信息已保存')
        handleCloseUserModal()
        loadUserInfo()
      } else {
        showMessageToast('保存失败')
      }
    } finally {
      setSaving(false)
    }
  }

  const handleOpenSettings = () => {
    setShowSettings(true)
  }

  const handleCloseSettings = () => {
    if (settingsRef.current) {
      gsap.to(settingsRef.current, {
        x: '100%',
        duration: 0.3,
        ease: 'power2.in',
        onComplete: () => {
          setShowSettings(false)
          loadUserInfo()
        },
      })
    } else {
      setShowSettings(false)
      loadUserInfo()
    }
  }

  const getAvatarUrl = () => {
    if (userInfo?.avatar) {
      return getUserAvatarUrl() + `?t=${Date.now()}`
    }
    return null
  }

  return (
    <div className="profile-page">
      <div className="profile-header">
        <div style={{ width: 44 }}></div>
        <div className="profile-title">我</div>
        <div style={{ width: 44 }}></div>
      </div>

      {loading ? (
        <div className="profile-loading">加载中...</div>
      ) : (
        <div className="profile-content">
          {/* 个人信息卡片 */}
          <div className="profile-card" onClick={handleOpenUserModal}>
            <div className="profile-avatar-wrapper">
              {getAvatarUrl() ? (
                <img src={getAvatarUrl()!} alt="Avatar" className="profile-avatar" />
              ) : (
                <div className="profile-avatar-default">
                  <svg viewBox="0 0 24 24">
                    <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" />
                  </svg>
                </div>
              )}
            </div>
            <div className="profile-info">
              <div className="profile-name">{userInfo?.username || '未设置昵称'}</div>
              <div className="profile-desc">{userInfo?.description || '暂无个人简介'}</div>
            </div>
            <svg className="profile-arrow" viewBox="0 0 24 24">
              <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
            </svg>
          </div>

          {/* 菜单列表 */}
          <div className="profile-menu-section">
            <div className="profile-menu-item" onClick={handleOpenSettings}>
              <div className="menu-icon settings-icon">
                <svg viewBox="0 0 24 24">
                  <path d="M19.14 12.94c.04-.31.06-.63.06-.94 0-.31-.02-.63-.06-.94l2.03-1.58c.18-.14.23-.41.12-.61l-1.92-3.32c-.12-.22-.37-.29-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54c-.04-.24-.24-.41-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.04.31-.06.63-.06.94s.02.63.06.94l-2.03 1.58c-.18.14-.23.41-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6c-1.98 0-3.6-1.62-3.6-3.6s1.62-3.6 3.6-3.6 3.6 1.62 3.6 3.6-1.62 3.6-3.6 3.6z" />
                </svg>
              </div>
              <span className="menu-label">设置</span>
              <svg className="menu-arrow" viewBox="0 0 24 24">
                <path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
              </svg>
            </div>
          </div>

          {message && (
            <div className={`profile-message ${message.includes('成功') || message.includes('已') ? 'success' : 'error'}`}>
              {message}
            </div>
          )}
        </div>
      )}

      {/* 设置二级界面 */}
      {showSettings && (
        <div className="settings-overlay" ref={settingsRef}>
          <Settings onBack={handleCloseSettings} />
        </div>
      )}

      {/* 用户信息编辑弹窗 */}
      {showUserModal && (
        <div className="profile-modal-overlay" onClick={handleCloseUserModal}>
          <div
            className="profile-modal-card"
            ref={userModalRef}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="profile-modal-header">
              <h3>编辑个人资料</h3>
              <button className="profile-modal-close" onClick={handleCloseUserModal}>
                <svg viewBox="0 0 24 24">
                  <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                </svg>
              </button>
            </div>

            <div className="profile-modal-body">
              {/* 头像上传 */}
              <div className="user-avatar-upload" onClick={handleUserAvatarClick}>
                {userAvatarPreview ? (
                  <img src={userAvatarPreview} alt="Avatar" className="user-avatar-preview" />
                ) : (
                  <div className="user-avatar-placeholder">
                    <svg viewBox="0 0 24 24">
                      <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" />
                    </svg>
                    <span>点击上传头像</span>
                  </div>
                )}
                <input
                  type="file"
                  ref={userFileInputRef}
                  onChange={handleUserFileChange}
                  accept="image/*"
                  style={{ display: 'none' }}
                />
              </div>

              {/* 用户名输入 */}
              <div className="form-group">
                <label>昵称</label>
                <input
                  type="text"
                  value={editingUserInfo.username}
                  onChange={(e) => setEditingUserInfo({ ...editingUserInfo, username: e.target.value })}
                  placeholder="输入你的昵称"
                />
              </div>

              {/* 自我描述输入 */}
              <div className="form-group">
                <label>个人简介</label>
                <textarea
                  value={editingUserInfo.description}
                  onChange={(e) => setEditingUserInfo({ ...editingUserInfo, description: e.target.value })}
                  placeholder="介绍一下你自己..."
                  rows={4}
                />
              </div>
            </div>

            <div className="profile-modal-footer">
              <button className="profile-modal-btn cancel" onClick={handleCloseUserModal}>
                取消
              </button>
              <button
                className="profile-modal-btn save"
                onClick={handleSaveUserInfo}
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

export default ProfilePage
