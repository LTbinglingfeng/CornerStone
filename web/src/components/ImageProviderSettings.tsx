import { useEffect, useMemo, useState } from 'react'
import { motion } from 'motion/react'
import type { Provider } from '../types/chat'
import { getProviders, setImageProvider } from '../services/api'
import { useT } from '../contexts/I18nContext'
import { getProviderTypesAll } from './provider'
import { useToast } from '../contexts/ToastContext'
import { drawerVariants } from '../utils/motion'
import { Group, Icon, Row } from './ui'
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
    const providerTypes = getProviderTypesAll()

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
    const check = (selected: boolean) =>
        selected ? (
            <Icon name="check" className="provider-choice-check" />
        ) : (
            <span className="provider-choice-placeholder" aria-hidden="true" />
        )
    const footer =
        imageProviders.length === 0
            ? t('imageProvider.noProviders')
            : !isAuto && !selectedProvider
              ? t('imageProvider.providerNotExist')
              : undefined

    return (
        <motion.div
            className="provider-settings"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={drawerVariants}
        >
            <div className="provider-settings-header">
                <button className="back-button" onClick={handleBack} aria-label={t('common.back')}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('imageProvider.title')}</div>
            </div>

            {loading ? (
                <div className="provider-settings-loading" role="status">
                    <span className="ui-spinner" aria-hidden="true" />
                    {t('common.loading')}
                </div>
            ) : (
                <div className="provider-settings-content">
                    <Group
                        title={t('settingsManagement.imageProvider.listTitle')}
                        description={t('imageProvider.hint')}
                        footer={footer}
                    >
                        <Row
                            icon="sparkles"
                            label={t('imageProvider.autoSelect')}
                            detail={
                                autoProvider
                                    ? `${autoProvider.name || autoProvider.id}${autoProvider.model ? ` · ${autoProvider.model}` : ''}`
                                    : t('settingsManagement.imageProvider.autoDetail')
                            }
                            aria-pressed={isAuto}
                            disabled={saving}
                            chevron={false}
                            trailing={check(isAuto)}
                            onClick={() => {
                                if (isAuto) return
                                void handleSelect('')
                            }}
                        />
                        {imageProviders.map((provider) => {
                            const isSelected = provider.id === imageProviderId
                            const typeLabel =
                                providerTypes.find((type) => type.value === provider.type)?.label ||
                                provider.type ||
                                t('provider.geminiImage')
                            return (
                                <Row
                                    key={provider.id}
                                    icon="image"
                                    label={provider.name || provider.id || t('common.unnamed')}
                                    detail={
                                        <>
                                            <span className="settings-mono">
                                                {provider.model || t('common.notSet')}
                                            </span>
                                            {` · ${typeLabel}`}
                                        </>
                                    }
                                    aria-pressed={isSelected}
                                    disabled={saving}
                                    chevron={false}
                                    trailing={check(isSelected)}
                                    onClick={() => {
                                        if (isSelected) return
                                        void handleSelect(provider.id)
                                    }}
                                />
                            )
                        })}
                    </Group>
                </div>
            )}
        </motion.div>
    )
}

export default ImageProviderSettings
