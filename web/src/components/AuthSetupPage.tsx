import { useState } from 'react'
import { useT } from '../contexts/I18nContext'
import { useTheme } from '../contexts/ThemeContext'
import { logoBlackDataUrl, logoWhiteDataUrl } from 'virtual:cornerstone-logos'
import './AuthSetupPage.css'

interface AuthSetupPageProps {
    onSubmit: (username: string, password: string) => Promise<string | null>
    loading?: boolean
}

const AuthSetupPage: React.FC<AuthSetupPageProps> = ({ onSubmit, loading = false }) => {
    const { t } = useT()
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
            setError(t('auth.enterUsername'))
            return
        }
        if (!password) {
            setError(t('auth.enterPasswordPlaceholder'))
            return
        }
        if (password !== confirmPassword) {
            setError(t('auth.passwordMismatch'))
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
                <div className="auth-title">{t('auth.setupAccount')}</div>
                <div className="auth-subtitle">{t('auth.firstLaunch')}</div>
                <form className="auth-form" onSubmit={handleSubmit}>
                    <div className="auth-field">
                        <label className="auth-label" htmlFor="auth-setup-username">
                            {t('auth.username')}
                        </label>
                        <input
                            id="auth-setup-username"
                            className="auth-input"
                            value={username}
                            onChange={(event) => {
                                if (error) setError(null)
                                setUsername(event.target.value)
                            }}
                            placeholder={t('auth.enterUsername')}
                            autoComplete="username"
                            disabled={loading}
                        />
                    </div>
                    <div className="auth-field">
                        <label className="auth-label" htmlFor="auth-setup-password">
                            {t('auth.password')}
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
                            placeholder={t('auth.enterPasswordPlaceholder')}
                            autoComplete="new-password"
                            disabled={loading}
                        />
                    </div>
                    <div className="auth-field">
                        <label className="auth-label" htmlFor="auth-setup-confirm">
                            {t('auth.confirmPassword')}
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
                            placeholder={t('auth.enterPasswordAgain')}
                            autoComplete="new-password"
                            disabled={loading}
                        />
                    </div>
                    {error && <div className="auth-error">{error}</div>}
                    <button className="auth-button" type="submit" disabled={loading}>
                        {loading ? t('auth.creating') : t('auth.createAccount')}
                    </button>
                </form>
            </div>
        </div>
    )
}

export default AuthSetupPage
