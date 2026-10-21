import { useEffect, useMemo, useRef, useState } from 'react'
import { getUserInfo, updateUserInfo, uploadUserAvatar, getUserAvatarUrl, appendQueryParam } from '../services/api'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { getAccountCopy } from '../i18n/account'
import type { UserInfo } from '../types/chat'
import { USER_UPDATED_EVENT } from './ConsoleNav'
import { Icon, Page, PageHeader } from './ui'
import './ProfilePage.css'

const ProfilePage: React.FC = () => {
    const { t, locale } = useT()
    const copy = getAccountCopy(locale).profile
    const { showToast } = useToast()
    const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
    const [loading, setLoading] = useState(true)
    const [form, setForm] = useState({ username: '', description: '' })
    const [avatarFile, setAvatarFile] = useState<File | null>(null)
    const [avatarPreview, setAvatarPreview] = useState<string | null>(null)
    const [saving, setSaving] = useState(false)
    const fileInputRef = useRef<HTMLInputElement>(null)

    const applyUserInfo = (info: UserInfo) => {
        setUserInfo(info)
        setForm({ username: info.username || '', description: info.description || '' })
    }

    useEffect(() => {
        let active = true
        void getUserInfo()
            .then((info) => {
                if (active && info) applyUserInfo(info)
            })
            .finally(() => {
                if (active) setLoading(false)
            })
        return () => {
            active = false
        }
    }, [])

    const savedAvatarUrl = useMemo(() => {
        if (!userInfo?.avatar) return null
        return appendQueryParam(getUserAvatarUrl(), 't', new Date(userInfo.updated_at).getTime())
    }, [userInfo?.avatar, userInfo?.updated_at])
    const avatarSrc = avatarPreview || savedAvatarUrl

    const dirty =
        avatarFile !== null ||
        form.username !== (userInfo?.username || '') ||
        form.description !== (userInfo?.description || '')

    const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
        const file = event.target.files?.[0]
        event.target.value = ''
        if (!file) return
        setAvatarFile(file)
        const reader = new FileReader()
        reader.onloadend = () => setAvatarPreview(reader.result as string)
        reader.readAsDataURL(file)
    }

    const handleSave = async (event: React.FormEvent) => {
        event.preventDefault()
        if (saving) return
        setSaving(true)
        try {
            const updated = await updateUserInfo(form)
            if (!updated) {
                showToast(t('common.saveFailed'), 'error')
                return
            }
            if (avatarFile) {
                await uploadUserAvatar(avatarFile)
            }
            applyUserInfo(updated)
            setAvatarFile(null)
            const latest = await getUserInfo()
            if (latest) applyUserInfo(latest)
            setAvatarPreview(null)
            window.dispatchEvent(new Event(USER_UPDATED_EVENT))
            showToast(t('profile.infoSaved'), 'success')
        } catch (error) {
            showToast(error instanceof Error ? error.message : t('common.saveFailed'), 'error')
        } finally {
            setSaving(false)
        }
    }

    const initial = (form.username || userInfo?.username || '').trim().charAt(0).toUpperCase()

    return (
        <Page className="profile-page">
            <PageHeader title={copy.title} description={copy.description} />

            {loading ? (
                <div className="profile-loading" role="status">
                    <span className="ui-spinner" aria-hidden="true" />
                    <span className="ui-visually-hidden">{t('common.loading')}</span>
                </div>
            ) : (
                <form className="profile-form ui-card" onSubmit={handleSave}>
                    <div className="profile-avatar-block">
                        <button
                            type="button"
                            className="profile-avatar"
                            onClick={() => fileInputRef.current?.click()}
                            aria-label={copy.changeAvatar}
                            disabled={saving}
                        >
                            {avatarSrc ? (
                                <img src={avatarSrc} alt="" />
                            ) : initial ? (
                                <span className="profile-avatar__initial">{initial}</span>
                            ) : (
                                <Icon name="user" size={30} />
                            )}
                            <span className="profile-avatar__badge" aria-hidden="true">
                                <Icon name="pencil" size={13} />
                            </span>
                        </button>
                        <div className="profile-avatar-text">
                            <button
                                type="button"
                                className="ui-btn ui-btn--sm"
                                onClick={() => fileInputRef.current?.click()}
                                disabled={saving}
                                tabIndex={-1}
                                aria-hidden="true"
                            >
                                {copy.changeAvatar}
                            </button>
                            <span className="ui-field__hint">{copy.avatarHint}</span>
                        </div>
                        <input type="file" ref={fileInputRef} onChange={handleFileChange} accept="image/*" hidden />
                    </div>

                    <div className="ui-field">
                        <label className="ui-field__label" htmlFor="profile-nickname">
                            {copy.nickname}
                        </label>
                        <input
                            id="profile-nickname"
                            className="ui-input"
                            type="text"
                            value={form.username}
                            onChange={(event) => setForm((current) => ({ ...current, username: event.target.value }))}
                            placeholder={t('profile.nicknamePlaceholder')}
                            autoComplete="nickname"
                            disabled={saving}
                        />
                    </div>

                    <div className="ui-field">
                        <label className="ui-field__label" htmlFor="profile-bio">
                            {copy.bio}
                        </label>
                        <textarea
                            id="profile-bio"
                            className="ui-textarea"
                            value={form.description}
                            onChange={(event) =>
                                setForm((current) => ({ ...current, description: event.target.value }))
                            }
                            placeholder={t('profile.bioPlaceholder')}
                            rows={5}
                            aria-describedby="profile-bio-hint"
                            disabled={saving}
                        />
                        <span className="ui-field__hint" id="profile-bio-hint">
                            {copy.bioHint}
                        </span>
                    </div>

                    <div className="profile-actions">
                        <button type="submit" className="ui-btn ui-btn--primary" disabled={saving || !dirty}>
                            {saving ? copy.saving : copy.save}
                        </button>
                    </div>
                </form>
            )}
        </Page>
    )
}

export default ProfilePage
