import { useEffect, useMemo, useState } from 'react'
import { motion } from 'motion/react'
import type { Provider } from '../types/chat'
import { getProviders, setImageProvider } from '../services/api'
import { PROVIDER_TYPES_ALL } from './provider'
import { useToast } from '../contexts/ToastContext'
import { drawerVariants } from '../utils/motion'
import './ProviderSettings.css'

interface ImageProviderSettingsProps {
    onBack: () => void
}

const ImageProviderSettings: React.FC<ImageProviderSettingsProps> = ({ onBack }) => {
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
            showToast(providerId ? '已切换生图供应商' : '已切换为自动选择', 'success')
        } else {
            showToast('设置失败', 'error')
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
                <div className="provider-settings-title">生图供应商</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">加载中...</div>
            ) : (
                <div className="provider-settings-content">
                    <div style={{ marginBottom: 12, color: 'var(--text-secondary)', fontSize: 12, lineHeight: 1.4 }}>
                        提示：生图供应商仅用于生图功能，不影响对话模型。需要先在「供应商管理」中添加类型为 Gemini
                        生图的供应商。
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
                                <div className="provider-card-id">自动选择</div>
                                {isAuto && <span className="active-indicator">使用中</span>}
                            </div>
                            <div className="provider-card-body">
                                <div className="provider-card-row">
                                    <span className="provider-card-label">当前</span>
                                    <span className="provider-card-value">{autoProvider?.name || '未配置'}</span>
                                </div>
                                <div className="provider-card-row">
                                    <span className="provider-card-label">模型</span>
                                    <span className="provider-card-value model">{autoProvider?.model || '未设置'}</span>
                                </div>
                            </div>
                        </div>

                        {imageProviders.length === 0 ? (
                            <div style={{ padding: 12, color: 'var(--text-secondary)', fontSize: 12 }}>
                                暂无可用的生图供应商
                            </div>
                        ) : (
                            imageProviders.map((provider) => {
                                const isSelected = provider.id === imageProviderId
                                const typeLabel =
                                    PROVIDER_TYPES_ALL.find((t) => t.value === provider.type)?.label ||
                                    provider.type ||
                                    'Gemini 生图'
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
                                            {isSelected && <span className="active-indicator">使用中</span>}
                                        </div>
                                        <div className="provider-card-body">
                                            <div className="provider-card-row">
                                                <span className="provider-card-label">名称</span>
                                                <span className="provider-card-value">{provider.name || '未命名'}</span>
                                            </div>
                                            <div className="provider-card-row">
                                                <span className="provider-card-label">模型</span>
                                                <span className="provider-card-value model">
                                                    {provider.model || '未设置'}
                                                </span>
                                            </div>
                                            <div className="provider-card-row">
                                                <span className="provider-card-label">类型</span>
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
                            当前选择的生图供应商不存在，请重新选择
                        </div>
                    )}
                </div>
            )}
        </motion.div>
    )
}

export default ImageProviderSettings
