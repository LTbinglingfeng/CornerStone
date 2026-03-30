import { useEffect, useMemo, useState } from 'react'
import { motion } from 'motion/react'
import type { Provider } from '../types/chat'
import { getProviders, setImageProvider } from '../services/api'
import { useT } from '../contexts/I18nContext'
import { getProviderTypesAll } from './provider'
import { useToast } from '../contexts/ToastContext'
import { drawerVariants } from '../utils/motion'
import './ProviderSettings.css'

interface ImageProviderSettingsProps {
    onBack: () => void
}

const ImageProviderSettings: React.FC<ImageProviderSettingsProps> = ({ onBack }) => {
    const { t } = useT()
    const { showToast } = useToast()
    const [providers, setProviders] = useState<Provider[]>([])
    const [imageProviderId, setImageProviderId] = useState('')
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)

    useEffect(() => {
        loadData()
    }, [])

    const imageProviders = useMemo(() => {
        return providers.filter((p) => p.type === 'gemini_image')
    }, [providers])

    const autoProvider = useMemo(() => {
        return imageProviders[0] || null
    }, [imageProviders])

    const selectedProvider = useMemo(() => {
        if (!imageProviderId) return null
        return imageProviders.find((p) => p.id === imageProviderId) || null
    }, [imageProviders, imageProviderId])

    const loadData = async () => {
        setLoading(true)
        const data = await getProviders()
        if (data) {
            setProviders(data.providers || [])
            setImageProviderId(data.image_provider_id || '')
        }
        setLoading(false)
    }

    const handleBack = () => {
        onBack()
    }

    const handleSelect = async (providerId: string) => {
        if (saving) return
        setSaving(true)
        const ok = await setImageProvider(providerId)
        if (ok) {
            setImageProviderId(providerId)
            showToast(providerId ? t('settings.imageProvider') : t('imageProvider.autoSelect'), 'success')
        } else {
            showToast(t('settings.settingFailed'), 'error')
        }
        setSaving(false)
    }

    const isAuto = imageProviderId === ''

    return (
        <motion.div
            className="provider-settings"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={drawerVariants}
        >
            <div className="provider-settings-header">
                <button className="back-button" onClick={handleBack}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('imageProvider.title')}</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : (
                <div className="provider-settings-content">
                    <div style={{ marginBottom: 12, color: 'var(--text-secondary)', fontSize: 12, lineHeight: 1.4 }}>
                        {t('imageProvider.hint')}
                    </div>

                    <div className="provider-cards">
                        <div
                            className={`provider-card ${isAuto ? 'active' : 'inactive'}`}
                            onClick={() => {
                                if (isAuto) return
                                handleSelect('')
                            }}
                        >
                            <div className="provider-card-header">
                                <div className="provider-card-id">{t('imageProvider.autoSelect')}</div>
                                {isAuto && <span className="active-indicator">{t('imageProvider.inUse')}</span>}
                            </div>
                            <div className="provider-card-body">
                                <div className="provider-card-row">
                                    <span className="provider-card-label">{t('common.current')}</span>
                                    <span className="provider-card-value">
                                        {autoProvider?.name || t('common.notConfigured')}
                                    </span>
                                </div>
                                <div className="provider-card-row">
                                    <span className="provider-card-label">{t('provider.model')}</span>
                                    <span className="provider-card-value model">
                                        {autoProvider?.model || t('common.notSet')}
                                    </span>
                                </div>
                            </div>
                        </div>

                        {imageProviders.length === 0 ? (
                            <div style={{ padding: 12, color: 'var(--text-secondary)', fontSize: 12 }}>
                                {t('imageProvider.noProviders')}
                            </div>
                        ) : (
                            imageProviders.map((provider) => {
                                const isSelected = provider.id === imageProviderId
                                const typeLabel =
                                    getProviderTypesAll().find((type) => type.value === provider.type)?.label ||
                                    provider.type ||
                                    t('provider.geminiImage')
                                return (
                                    <div
                                        key={provider.id}
                                        className={`provider-card ${isSelected ? 'active' : 'inactive'}`}
                                        onClick={() => {
                                            if (isSelected) return
                                            handleSelect(provider.id)
                                        }}
                                    >
                                        <div className="provider-card-header">
                                            <div className="provider-card-id">{provider.id}</div>
                                            {isSelected && (
                                                <span className="active-indicator">{t('imageProvider.inUse')}</span>
                                            )}
                                        </div>
                                        <div className="provider-card-body">
                                            <div className="provider-card-row">
                                                <span className="provider-card-label">{t('imageProvider.name')}</span>
                                                <span className="provider-card-value">
                                                    {provider.name || t('common.unnamed')}
                                                </span>
                                            </div>
                                            <div className="provider-card-row">
                                                <span className="provider-card-label">{t('provider.model')}</span>
                                                <span className="provider-card-value model">
                                                    {provider.model || t('common.notSet')}
                                                </span>
                                            </div>
                                            <div className="provider-card-row">
                                                <span className="provider-card-label">{t('imageProvider.type')}</span>
                                                <span className="provider-card-value type">{typeLabel}</span>
                                            </div>
                                        </div>
                                    </div>
                                )
                            })
                        )}
                    </div>

                    {!isAuto && !selectedProvider && (
                        <div style={{ marginTop: 12, color: 'var(--text-secondary)', fontSize: 12 }}>
                            {t('imageProvider.providerNotExist')}
                        </div>
                    )}
                </div>
            )}
        </motion.div>
    )
}

export default ImageProviderSettings
