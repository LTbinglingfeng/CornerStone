import { useEffect, useState } from 'react'
import { useTheme } from '../contexts/ThemeContext'
import { logoBlackDataUrl, logoWhiteDataUrl } from 'virtual:cornerstone-logos'
import './AuthLoginPage.css'

interface AuthLoginPageProps {
  username?: string | null
  onSubmit: (username: string, password: string) => Promise<string | null>
  loading?: boolean
}

const AuthLoginPage: React.FC<AuthLoginPageProps> = ({ username, onSubmit, loading = false }) => {
  const { theme } = useTheme()
  const [inputUsername, setInputUsername] = useState(username || '')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)

  const logoSrc = theme === 'dark' ? logoBlackDataUrl : logoWhiteDataUrl

  useEffect(() => {
    if (username) {
      setInputUsername(username)
    }
  }, [username])

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    const trimmed = inputUsername.trim()
    if (!trimmed) {
      setError('请输入用户名')
      return
    }
    if (!password) {
      setError('请输入密码')
      return
    }
    setError(null)
    const submitError = await onSubmit(trimmed, password)
    if (submitError) {
      setError(submitError)
    }
  }

  const isUsernameLocked = Boolean(username)

  return (
    <div className="auth-page">
      <div className="auth-card">
        <div className="auth-logo-wrapper">
          <img className="auth-logo" src={logoSrc} alt="CornerStone" />
        </div>
        <div className="auth-title">输入密码</div>
        <div className="auth-subtitle">欢迎回来，请验证密码</div>
        <form className="auth-form" onSubmit={handleSubmit}>
          <div className="auth-field">
            <label className="auth-label" htmlFor="auth-login-username">用户名</label>
            <input
              id="auth-login-username"
              className="auth-input"
              value={inputUsername}
              onChange={(event) => {
                if (error) setError(null)
                setInputUsername(event.target.value)
              }}
              placeholder="请输入用户名"
              autoComplete="username"
              disabled={loading || isUsernameLocked}
            />
          </div>
          <div className="auth-field">
            <label className="auth-label" htmlFor="auth-login-password">密码</label>
            <input
              id="auth-login-password"
              className="auth-input"
              type="password"
              value={password}
              onChange={(event) => {
                if (error) setError(null)
                setPassword(event.target.value)
              }}
              placeholder="请输入密码"
              autoComplete="current-password"
              disabled={loading}
            />
          </div>
          {error && <div className="auth-error">{error}</div>}
          <button className="auth-button" type="submit" disabled={loading}>
            {loading ? '验证中...' : '进入应用'}
          </button>
        </form>
      </div>
    </div>
  )
}

export default AuthLoginPage
