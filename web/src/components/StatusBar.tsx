import './StatusBar.css'

const StatusBar: React.FC = () => {
    return (
        <div className="status-bar">
            <div className="status-bar-left">
                <span>9:41</span>
            </div>
            <div className="status-bar-right">
                <svg width="18" height="12" viewBox="0 0 18 12" fill="white">
                    <path d="M1 4.5C1 3.67 1.67 3 2.5 3H3.5C4.33 3 5 3.67 5 4.5V10.5C5 11.33 4.33 12 3.5 12H2.5C1.67 12 1 11.33 1 10.5V4.5Z" />
                    <path d="M6 3C6 2.17 6.67 1.5 7.5 1.5H8.5C9.33 1.5 10 2.17 10 3V10.5C10 11.33 9.33 12 8.5 12H7.5C6.67 12 6 11.33 6 10.5V3Z" />
                    <path d="M11 1.5C11 0.67 11.67 0 12.5 0H13.5C14.33 0 15 0.67 15 1.5V10.5C15 11.33 14.33 12 13.5 12H12.5C11.67 12 11 11.33 11 10.5V1.5Z" />
                </svg>
                <svg width="16" height="11" viewBox="0 0 16 11" fill="white">
                    <path d="M8 1.5C10.5 1.5 12.7 2.6 14.2 4.4L15.6 3C13.7 0.9 11 -0.3 8 -0.3C5 -0.3 2.3 0.9 0.4 3L1.8 4.4C3.3 2.6 5.5 1.5 8 1.5Z" />
                    <path d="M8 4.5C9.7 4.5 11.2 5.2 12.3 6.4L13.7 5C12.2 3.5 10.2 2.5 8 2.5C5.8 2.5 3.8 3.5 2.3 5L3.7 6.4C4.8 5.2 6.3 4.5 8 4.5Z" />
                    <path d="M8 7.5C9 7.5 9.9 7.9 10.5 8.6L11.9 7.2C10.9 6.2 9.5 5.5 8 5.5C6.5 5.5 5.1 6.2 4.1 7.2L5.5 8.6C6.1 7.9 7 7.5 8 7.5Z" />
                    <circle cx="8" cy="10" r="1.5" />
                </svg>
                <svg width="25" height="12" viewBox="0 0 25 12" fill="white">
                    <rect x="0.5" y="0.5" width="21" height="11" rx="2.5" stroke="white" strokeWidth="1" fill="none" />
                    <rect x="2" y="2" width="18" height="8" rx="1" fill="white" />
                    <path d="M23 4V8C23.8 7.6 24.5 6.9 24.5 6C24.5 5.1 23.8 4.4 23 4Z" fill="white" />
                </svg>
            </div>
        </div>
    )
}

export default StatusBar
