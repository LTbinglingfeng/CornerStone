import { Link, NavLink } from 'react-router-dom'
import { useT } from '../contexts/I18nContext'
import { useTheme } from '../contexts/ThemeContext'
import { tabOrder, tabRoutes, type AppTab } from '../utils/routes'

const icons: Record<AppTab, string> = {
    overview: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
    channels: 'M8 7h8 M8 12h5 M21 11a8 8 0 0 1-8 8H7l-4 3V11a8 8 0 0 1 8-8h2a8 8 0 0 1 8 8Z',
    contacts: 'M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z M4 21v-2a8 8 0 0 1 16 0v2',
    chat: 'M6 3h12v18H6z M9 7h6 M9 11h6 M9 15h4',
    settings: 'M4 6h16 M4 12h16 M4 18h16 M8 3v6 M16 9v6 M10 15v6',
    me: 'M16 9a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z M5 21a7 7 0 0 1 14 0',
}

export function ConsoleIcon({ name }: { name: AppTab }) {
    return (
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
        >
            <path d={icons[name]} />
        </svg>
    )
}

export default function ConsoleNav() {
    const { t } = useT()
    const { theme, toggleTheme } = useTheme()
    return (
        <aside className="console-sidebar">
            <Link to={tabRoutes.overview} className="console-brand">
                <span className="console-mark" aria-hidden="true">
                    C<span>·</span>
                </span>
                <span>
                    CornerStone<small>{t('console.title')}</small>
                </span>
            </Link>
            <nav className="console-nav" aria-label={t('console.title')}>
                {tabOrder.map((tab) => (
                    <NavLink key={tab} to={tabRoutes[tab]} aria-label={t(`console.${tab}`)}>
                        <ConsoleIcon name={tab} />
                        <span className="console-nav-label">{t(`console.${tab}`)}</span>
                        <span className="console-nav-short">{t(`console.short.${tab}`)}</span>
                    </NavLink>
                ))}
            </nav>
            <div className="console-sidebar-footer">
                <span className="console-version">CornerStone · {__CORNERSTONE_VERSION__.trim() || 'dev'}</span>
                <button
                    type="button"
                    className="console-theme"
                    onClick={toggleTheme}
                    aria-label={t('console.theme')}
                    title={t('console.theme')}
                >
                    <span aria-hidden="true">{theme === 'dark' ? '☀' : '☾'}</span>
                </button>
            </div>
        </aside>
    )
}
