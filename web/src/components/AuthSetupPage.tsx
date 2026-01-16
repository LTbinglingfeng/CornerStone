import { useState } from 'react'
import { useTheme } from '../contexts/ThemeContext'
import { logoBlackDataUrl, logoWhiteDataUrl } from 'virtual:cornerstone-logos'
import './AuthSetupPage.css'

interface AuthSetupPageProps {
    onSubmit: (username: string, password: string) => Promise<string | null>
    loading?: boolean
}

const AuthSetupPage: React.FC<AuthSetupPageProps> = ({ onSubmit, loading = false }) => {
    const { theme } = useTheme()
    const [username, setUsername] = useState('')
    const [password, setPassword] = useState('')
    const [confirmPassword, setConfirmPassword] = useState('')
    const [error, setError] = useState<string | null>(null)

    const logoSrc = theme === 'dark' ? logoBlackDataUrl : logoWhiteDataUrl

    const handleSubmit = async (event: React.FormEvent) => {
        event.preventDefault()
        const trimmed = username.trim()
        if (!trimmed) {
            setError('请输入用户名')
            return
        }
        if (!password) {
            setError('请输入密码')
            return
        }
        if (password !== confirmPassword) {
            setError('两次密码不一致')
            return
        }
        setError(null)
        const submitError = await onSubmit(trimmed, password)
        if (submitError) {
            setError(submitError)
        }
    }

    return (
        <div className="auth-page">
            <div className="auth-card">
                <div className="auth-logo-wrapper">
                    <img className="auth-logo" src={logoSrc} alt="CornerStone" />
                </div>
                <div className="auth-title">设置账号</div>
                <div className="auth-subtitle">首次启动，请创建用户名与密码</div>
                <form className="auth-form" onSubmit={handleSubmit}>
                    <div className="auth-field">
                        <label className="auth-label" htmlFor="auth-setup-username">
                            用户名
                        </label>
                        <input
                            id="auth-setup-username"
                            className="auth-input"
                            value={username}
                            onChange={(event) => {
                                if (error) setError(null)
                                setUsername(event.target.value)
                            }}
                            placeholder="请输入用户名"
                            autoComplete="username"
                            disabled={loading}
                        />
                    </div>
                    <div className="auth-field">
                        <label className="auth-label" htmlFor="auth-setup-password">
                            密码
                        </label>
                        <input
                            id="auth-setup-password"
                            className="auth-input"
                            type="password"
                            value={password}
                            onChange={(event) => {
                                if (error) setError(null)
                                setPassword(event.target.value)
                            }}
                            placeholder="请输入密码"
                            autoComplete="new-password"
                            disabled={loading}
                        />
                    </div>
                    <div className="auth-field">
                        <label className="auth-label" htmlFor="auth-setup-confirm">
                            确认密码
                        </label>
                        <input
                            id="auth-setup-confirm"
                            className="auth-input"
                            type="password"
                            value={confirmPassword}
                            onChange={(event) => {
                                if (error) setError(null)
                                setConfirmPassword(event.target.value)
                            }}
                            placeholder="再次输入密码"
                            autoComplete="new-password"
                            disabled={loading}
                        />
                    </div>
                    {error && <div className="auth-error">{error}</div>}
                    <button className="auth-button" type="submit" disabled={loading}>
                        {loading ? '创建中...' : '创建账号'}
                    </button>
                </form>
            </div>
        </div>
    )
}

export default AuthSetupPage
