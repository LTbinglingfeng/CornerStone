import { useEffect, useState } from 'react'
import { useT } from '../contexts/I18nContext'
import { useTheme } from '../contexts/ThemeContext'
import { logoBlackDataUrl, logoWhiteDataUrl } from 'virtual:cornerstone-logos'
import { getAccountCopy } from '../i18n/account'
import './AuthLoginPage.css'

interface AuthLoginPageProps {
    username?: string | null
    onSubmit: (username: string, password: string) => Promise<string | null>
    loading?: boolean
}

const AuthLoginPage: React.FC<AuthLoginPageProps> = ({ username, onSubmit, loading = false }) => {
    const { t, locale } = useT()
    const copy = getAccountCopy(locale).login
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
            setError(t('auth.enterUsername'))
            return
        }
        if (!password) {
            setError(t('auth.enterPasswordPlaceholder'))
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
                <img className="auth-logo" src={logoSrc} alt="CornerStone" />
                <h1 className="auth-title">{copy.title}</h1>
                <p className="auth-subtitle">{copy.subtitle}</p>
                <form className="auth-form" onSubmit={handleSubmit}>
                    <div className="ui-field auth-field">
                        <label className="ui-field__label auth-label" htmlFor="auth-login-username">
                            {t('auth.username')}
                        </label>
                        <input
                            id="auth-login-username"
                            className="ui-input auth-input"
                            value={inputUsername}
                            onChange={(event) => {
                                if (error) setError(null)
                                setInputUsername(event.target.value)
                            }}
                            placeholder={t('auth.enterUsername')}
                            autoComplete="username"
                            disabled={loading || isUsernameLocked}
                        />
                    </div>
                    <div className="ui-field auth-field">
                        <label className="ui-field__label auth-label" htmlFor="auth-login-password">
                            {t('auth.password')}
                        </label>
                        <input
                            id="auth-login-password"
                            className="ui-input auth-input"
                            type="password"
                            value={password}
                            onChange={(event) => {
                                if (error) setError(null)
                                setPassword(event.target.value)
                            }}
                            placeholder={t('auth.enterPasswordPlaceholder')}
                            autoComplete="current-password"
                            disabled={loading}
                        />
                    </div>
                    {error && (
                        <div className="auth-error" role="alert">
                            {error}
                        </div>
                    )}
                    <button
                        className="auth-button ui-btn ui-btn--primary ui-btn--lg ui-btn--block"
                        type="submit"
                        disabled={loading}
                    >
                        {loading ? t('auth.verifying') : copy.submit}
                    </button>
                </form>
            </div>
        </div>
    )
}

export default AuthLoginPage
