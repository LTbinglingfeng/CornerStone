import './BottomNav.css'

interface BottomNavProps {
    activeTab: 'chat' | 'contacts' | 'moments' | 'settings'
    onTabChange: (tab: 'chat' | 'contacts' | 'moments' | 'settings') => void
}

const BottomNav: React.FC<BottomNavProps> = ({ activeTab, onTabChange }) => {
    return (
        <div className="bottom-nav">
            <div className={`nav-item ${activeTab === 'chat' ? 'active' : ''}`} onClick={() => onTabChange('chat')}>
                <div className="nav-icon">
                    <svg viewBox="0 0 24 24">
                        <path d="M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm0 14H6l-2 2V4h16v12z" />
                    </svg>
                </div>
                <span className="nav-label">聊天</span>
            </div>
            <div
                className={`nav-item ${activeTab === 'contacts' ? 'active' : ''}`}
                onClick={() => onTabChange('contacts')}
            >
                <div className="nav-icon">
                    <svg viewBox="0 0 24 24">
                        <path d="M19 3h-4.18C14.4 1.84 13.3 1 12 1c-1.3 0-2.4.84-2.82 2H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-7 0c.55 0 1 .45 1 1s-.45 1-1 1-1-.45-1-1 .45-1 1-1zm0 4c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm6 12H6v-1.4c0-2 4-3.1 6-3.1s6 1.1 6 3.1V19z" />
                    </svg>
                </div>
                <span className="nav-label">通讯录</span>
            </div>
            <div
                className={`nav-item ${activeTab === 'moments' ? 'active' : ''}`}
                onClick={() => onTabChange('moments')}
            >
                <div className="nav-icon">
                    <svg viewBox="0 0 24 24">
                        <path d="M21 19V5c0-1.1-.9-2-2-2H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2zM8.5 13.5l2.5 3.01L14.5 12l4.5 6H5l3.5-4.5z" />
                    </svg>
                </div>
                <span className="nav-label">朋友圈</span>
            </div>
            <div
                className={`nav-item ${activeTab === 'settings' ? 'active' : ''}`}
                onClick={() => onTabChange('settings')}
            >
                <div className="nav-icon">
                    <svg viewBox="0 0 24 24">
                        <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" />
                    </svg>
                </div>
                <span className="nav-label">我</span>
            </div>
        </div>
    )
}

export default BottomNav
