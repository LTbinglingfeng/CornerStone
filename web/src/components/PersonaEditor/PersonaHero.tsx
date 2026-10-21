import { useId, useLayoutEffect, useRef } from 'react'
import { useT } from '../../contexts/I18nContext'
import { getPersonaCopy } from '../../i18n/personas'
import { Icon } from '../ui'

interface PersonaHeroProps {
    name: string
    description: string
    avatarUrl: string | null
    onNameChange: (name: string) => void
    onDescriptionChange: (description: string) => void
    onAvatarChange: (file: File) => void
    onAvatarDelete: () => void
}

/** Profile-card style identity block: avatar, name and a one-line description. */
const PersonaHero: React.FC<PersonaHeroProps> = ({
    name,
    description,
    avatarUrl,
    onNameChange,
    onDescriptionChange,
    onAvatarChange,
    onAvatarDelete,
}) => {
    const { locale } = useT()
    const copy = getPersonaCopy(locale).editor
    const fileInputRef = useRef<HTMLInputElement>(null)
    const descriptionRef = useRef<HTMLTextAreaElement>(null)
    const nameId = useId()
    const descriptionId = useId()

    const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0]
        if (file) {
            onAvatarChange(file)
        }
        // 重置 input 以便选择同一文件
        e.target.value = ''
    }

    // 自适应 textarea 高度（包括加载后的初始值）
    useLayoutEffect(() => {
        const el = descriptionRef.current
        if (!el) return
        el.style.height = 'auto'
        el.style.height = `${el.scrollHeight}px`
    }, [description])

    const initial = name.trim().charAt(0).toUpperCase()

    return (
        <section className="persona-identity">
            <div className="persona-avatar">
                <button
                    type="button"
                    className="persona-avatar__button"
                    onClick={() => fileInputRef.current?.click()}
                    aria-label={copy.changeAvatarLabel}
                >
                    {avatarUrl ? (
                        <img className="persona-avatar__image" src={avatarUrl} alt="" />
                    ) : (
                        <span className="persona-avatar__initial" aria-hidden="true">
                            {initial || <Icon name="camera" size={24} />}
                        </span>
                    )}
                    <span className="persona-avatar__overlay" aria-hidden="true">
                        <Icon name="camera" size={16} />
                        {copy.changeAvatar}
                    </span>
                </button>
                <input
                    type="file"
                    ref={fileInputRef}
                    onChange={handleFileChange}
                    accept="image/*"
                    tabIndex={-1}
                    aria-hidden="true"
                    hidden
                />
                {avatarUrl && (
                    <button type="button" className="persona-avatar__remove" onClick={onAvatarDelete}>
                        {copy.removeAvatar}
                    </button>
                )}
            </div>
            <div className="persona-identity__fields">
                <label htmlFor={nameId} className="ui-visually-hidden">
                    {copy.nameLabel}
                </label>
                <input
                    id={nameId}
                    required
                    className="name-input"
                    type="text"
                    value={name}
                    onChange={(e) => onNameChange(e.target.value)}
                    placeholder={copy.namePlaceholder}
                    maxLength={50}
                    autoComplete="off"
                />
                <label htmlFor={descriptionId} className="ui-visually-hidden">
                    {copy.descriptionLabel}
                </label>
                <textarea
                    id={descriptionId}
                    ref={descriptionRef}
                    className="description-input"
                    value={description}
                    onChange={(e) => onDescriptionChange(e.target.value)}
                    placeholder={copy.descriptionPlaceholder}
                    rows={1}
                    maxLength={200}
                />
            </div>
        </section>
    )
}

export default PersonaHero
