import { useCallback, useEffect, useRef, useState } from 'react'
import type { UserInfo } from '../../types/chat'
import type { Moment } from '../../types/moments'
import { useT } from '../../contexts/I18nContext'
import { getUserAvatarUrl, getUserInfo } from '../../services/api'
import { getMoments, getMomentsConfig, uploadBackground } from '../../services/moments'
import MomentCard from './MomentCard'
import MomentDetail from './MomentDetail'
import './MomentsPage.css'

function normalizeAssetPath(path?: string): string {
    const trimmed = (path || '').trim()
    if (!trimmed) return ''
    return trimmed.startsWith('/') ? trimmed : `/${trimmed}`
}

const MomentsPage: React.FC = () => {
    const { t } = useT()
    const [moments, setMoments] = useState<Moment[]>([])
    const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
    const [backgroundImage, setBackgroundImage] = useState('')
    const [selectedMoment, setSelectedMoment] = useState<Moment | null>(null)
    const [loading, setLoading] = useState(true)
    const fileInputRef = useRef<HTMLInputElement>(null)

    const refresh = useCallback(async () => {
        const [momentsData, config, user] = await Promise.all([getMoments(), getMomentsConfig(), getUserInfo()])
        setMoments(momentsData)
        setBackgroundImage(normalizeAssetPath(config.background_image))
        setUserInfo(user)
    }, [])

    useEffect(() => {
        let cancelled = false
        ;(async () => {
            await refresh()
            if (!cancelled) setLoading(false)
        })()
        return () => {
            cancelled = true
        }
    }, [refresh])

    useEffect(() => {
        const intervalId = window.setInterval(() => {
            void refresh()
        }, 10000)
        return () => window.clearInterval(intervalId)
    }, [refresh])

    const handlePickBackground = () => {
        fileInputRef.current?.click()
    }

    const handleBackgroundChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0]
        e.target.value = ''
        if (!file) return

        const path = await uploadBackground(file)
        if (path) {
            setBackgroundImage(normalizeAssetPath(path))
        }
    }

    const handleDetailClose = () => {
        setSelectedMoment(null)
        void refresh()
    }

    const username = userInfo?.username?.trim() || t('moments.user')
    const userAvatarSrc = userInfo?.avatar ? `${getUserAvatarUrl()}?t=${encodeURIComponent(userInfo.updated_at)}` : ''

    if (loading) {
        return <div className="moments-loading">{t('common.loading')}</div>
    }

    return (
        <div className="moments-page">
            <div
                className="moments-header"
                style={backgroundImage ? { backgroundImage: `url(${backgroundImage})` } : undefined}
            >
                <div className="moments-header-overlay" />
                <button className="moments-bg-upload" type="button" onClick={handlePickBackground}>
                    {t('moments.changeBackground')}
                </button>
                <input
                    ref={fileInputRef}
                    type="file"
                    accept="image/*"
                    onChange={handleBackgroundChange}
                    style={{ display: 'none' }}
                />
                <div className="moments-user-info">
                    <span className="moments-username">{username}</span>
                    {userAvatarSrc ? (
                        <img className="moments-user-avatar" src={userAvatarSrc} alt="avatar" />
                    ) : (
                        <div className="moments-user-avatar moments-user-avatar-placeholder">
                            {username.charAt(0).toUpperCase()}
                        </div>
                    )}
                </div>
            </div>

            <div className="moments-list">
                {moments.map((moment) => (
                    <MomentCard
                        key={moment.id}
                        moment={moment}
                        userInfo={userInfo}
                        onClick={() => setSelectedMoment(moment)}
                        onRefresh={refresh}
                    />
                ))}
                {moments.length === 0 && <div className="moments-empty">{t('moments.noMoments')}</div>}
            </div>

            {selectedMoment && (
                <MomentDetail
                    moment={selectedMoment}
                    userInfo={userInfo}
                    onClose={handleDetailClose}
                    onRefresh={refresh}
                />
            )}
        </div>
    )
}

export default MomentsPage
