import { useRef } from 'react'

interface PersonaHeroProps {
    name: string
    description: string
    avatarUrl: string | null
    onNameChange: (name: string) => void
    onDescriptionChange: (description: string) => void
    onAvatarChange: (file: File) => void
    onAvatarDelete: () => void
}

const PersonaHero: React.FC<PersonaHeroProps> = ({
    name,
    description,
    avatarUrl,
    onNameChange,
    onDescriptionChange,
    onAvatarChange,
    onAvatarDelete,
}) => {
    const fileInputRef = useRef<HTMLInputElement>(null)
    const descRef = useRef<HTMLTextAreaElement>(null)

    const handleAvatarClick = () => {
        fileInputRef.current?.click()
    }

    const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0]
        if (file) {
            onAvatarChange(file)
        }
        // 重置 input 以便选择同一文件
        e.target.value = ''
    }

    // 自适应 textarea 高度
    const handleDescriptionInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
        onDescriptionChange(e.target.value)
        const el = e.target
        el.style.height = 'auto'
        el.style.height = el.scrollHeight + 'px'
    }

    const initial = name ? name.charAt(0).toUpperCase() : '?'

    return (
        <div className="persona-hero">
            {/* 头像 */}
            <div className="avatar-container" onClick={handleAvatarClick}>
                {avatarUrl ? (
                    <img className="avatar-image" src={avatarUrl} alt={name || '头像'} />
                ) : (
                    <div className="avatar-fallback">{initial}</div>
                )}
                <div className="avatar-overlay">
                    <svg viewBox="0 0 24 24">
                        <path d="M12 15.2a3.2 3.2 0 1 0 0-6.4 3.2 3.2 0 0 0 0 6.4z" />
                        <path d="M9 2 7.17 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2h-3.17L15 2H9zm3 15c-2.76 0-5-2.24-5-5s2.24-5 5-5 5 2.24 5 5-2.24 5-5 5z" />
                    </svg>
                </div>
                <input
                    type="file"
                    ref={fileInputRef}
                    onChange={handleFileChange}
                    accept="image/*"
                    style={{ display: 'none' }}
                />
            </div>

            {/* 删除头像 */}
            {avatarUrl && (
                <button
                    className="avatar-delete-link"
                    onClick={(e) => {
                        e.stopPropagation()
                        onAvatarDelete()
                    }}
                >
                    删除头像
                </button>
            )}

            {/* 名称 */}
            <input
                className="name-input"
                type="text"
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                placeholder="角色名称"
                maxLength={50}
            />

            {/* 描述 */}
            <textarea
                ref={descRef}
                className="description-input"
                value={description}
                onChange={handleDescriptionInput}
                placeholder="添加描述..."
                rows={1}
                maxLength={200}
            />
        </div>
    )
}

export default PersonaHero
