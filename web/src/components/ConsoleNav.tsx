import { useCallback, useEffect, useState } from 'react'
import { Link, NavLink, useLocation } from 'react-router-dom'
import { logoBlackDataUrl } from 'virtual:cornerstone-logos'
import { useT } from '../contexts/I18nContext'
import { useTheme } from '../contexts/ThemeContext'
import { appendQueryParam, getUserAvatarUrl, getUserInfo } from '../services/api'
import type { UserInfo } from '../types/chat'
import { navTabs, settingsChildTabs, sidebarTabs, tabRoutes, type AppTab } from '../utils/routes'
import { Avatar, Icon, type IconName } from './ui'

const tabIcons: Record<AppTab, IconName> = {
    overview: 'overview',
    chat: 'chat',
    contacts: 'persona',
    channels: 'channels',
    usage: 'chart',
    logs: 'terminal',
    settings: 'settings',
    me: 'user',
}

/** Dispatched by the profile page after saving so the account entry stays current. */
export const USER_UPDATED_EVENT = 'cornerstone:user-updated'

export function ConsoleIcon({ name }: { name: AppTab }) {
    return <Icon name={tabIcons[name]} />
}

function useAccount(fallbackName: string | null) {
    const [user, setUser] = useState<UserInfo | null>(null)
    const load = useCallback(() => {
        void getUserInfo().then((info) => {
            if (info) setUser(info)
        })
    }, [])
    useEffect(() => {
        load()
        window.addEventListener(USER_UPDATED_EVENT, load)
        return () => window.removeEventListener(USER_UPDATED_EVENT, load)
    }, [load])
    const name = user?.username || fallbackName || ''
    const avatar = user?.avatar ? appendQueryParam(getUserAvatarUrl(), 't', new Date(user.updated_at).getTime()) : null
    return { name, avatar }
}

export default function ConsoleNav({ username }: { username?: string | null }) {
    const { t } = useT()
    const { theme, toggleTheme } = useTheme()
    const account = useAccount(username ?? null)
    const { pathname } = useLocation()
    // Usage and logs are reached from Settings on mobile, so Settings stays highlighted there.
    const inSettingsChild = settingsChildTabs.some(
        (tab) => pathname === tabRoutes[tab] || pathname.startsWith(`${tabRoutes[tab]}/`)
    )
    const themeLabel = theme === 'dark' ? t('console.lightMode') : t('console.darkMode')

    return (
        <>
            <aside className="console-sidebar">
                <Link to={tabRoutes.overview} className="console-brand" aria-label="CornerStone">
                    <img className="console-mark" src={logoBlackDataUrl} alt="" width={28} height={28} />
                    <span>CornerStone</span>
                </Link>
                <nav className="console-nav" aria-label={t('console.title')}>
                    {sidebarTabs.map((tab) => (
                        <NavLink key={tab} to={tabRoutes[tab]}>
                            <ConsoleIcon name={tab} />
                            <span>{t(`console.${tab}`)}</span>
                        </NavLink>
                    ))}
                </nav>
                <div className="console-sidebar-footer">
                    <NavLink to={tabRoutes.me} className="console-account" aria-label={t('console.me')}>
                        <Avatar src={account.avatar} name={account.name || '?'} size="sm" />
                        <span className="console-account-name">{account.name || t('console.me')}</span>
                    </NavLink>
                    <button
                        type="button"
                        className="ui-icon-btn console-theme"
                        onClick={toggleTheme}
                        aria-label={t('console.theme')}
                        title={themeLabel}
                    >
                        <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
                    </button>
                </div>
            </aside>
            <nav className="console-tabbar" aria-label={t('console.title')}>
                {navTabs.map((tab) => (
                    <NavLink
                        key={tab}
                        to={tabRoutes[tab]}
                        aria-label={t(`console.${tab}`)}
                        className={({ isActive }) =>
                            isActive || (tab === 'settings' && inSettingsChild) ? 'active' : undefined
                        }
                    >
                        <ConsoleIcon name={tab} />
                        <span>{t(`console.short.${tab}`)}</span>
                    </NavLink>
                ))}
            </nav>
        </>
    )
}
