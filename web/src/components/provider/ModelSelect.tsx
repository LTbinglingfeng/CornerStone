import { useState, useEffect, useRef, useCallback } from 'react'
import { useT } from '../../contexts/I18nContext'
import { fetchProviderModels } from '../../services/api'

interface ModelSelectProps {
    value: string
    providerId: string
    providerType: string
    baseUrl: string
    apiKey: string
    isNewProvider: boolean
    placeholder?: string
    onChange: (value: string) => void
    onError?: (message: string) => void
}

const ModelSelect: React.FC<ModelSelectProps> = ({
    value,
    providerId,
    providerType,
    baseUrl,
    apiKey,
    isNewProvider,
    placeholder,
    onChange,
    onError,
}) => {
    const { t } = useT()
    const [open, setOpen] = useState(false)
    const [models, setModels] = useState<string[]>([])
    const [loading, setLoading] = useState(false)
    const [search, setSearch] = useState('')
    const [hasFetched, setHasFetched] = useState(false)
    const wrapperRef = useRef<HTMLDivElement>(null)
    const searchRef = useRef<HTMLInputElement>(null)

    // Close dropdown on outside click
    useEffect(() => {
        if (!open) return
        const handleClickOutside = (event: MouseEvent) => {
            if (!wrapperRef.current) return
            if (!wrapperRef.current.contains(event.target as Node)) {
                setOpen(false)
                setSearch('')
            }
        }
        document.addEventListener('mousedown', handleClickOutside)
        return () => document.removeEventListener('mousedown', handleClickOutside)
    }, [open])

    // Auto-focus search when dropdown opens
    useEffect(() => {
        if (open && searchRef.current) {
            searchRef.current.focus()
        }
    }, [open])

    // 当 type/url 变化时，清除已缓存的模型列表
    const configKeyRef = useRef(`${providerType}|${baseUrl}`)
    useEffect(() => {
        const key = `${providerType}|${baseUrl}`
        if (key !== configKeyRef.current) {
            configKeyRef.current = key
            setModels([])
            setHasFetched(false)
            setOpen(false)
        }
    }, [providerType, baseUrl])

    const canFetch = !!providerType && !!baseUrl && (!isNewProvider || !!apiKey)

    const handleFetch = useCallback(async () => {
        if (loading) return
        if (!canFetch) {
            onError?.(t('provider.fetchModelsFailed'))
            return
        }
        setLoading(true)
        try {
            const result = await fetchProviderModels(providerId || '_new', {
                type: providerType,
                base_url: baseUrl,
                api_key: apiKey,
            })
            setModels(result)
            setHasFetched(true)
            if (result.length > 0) {
                setOpen(true)
                setSearch('')
            } else {
                onError?.(t('provider.noModelsFound'))
            }
        } catch (err) {
            const message = err instanceof Error ? err.message : t('provider.fetchModelsFailed')
            onError?.(message)
        } finally {
            setLoading(false)
        }
    }, [loading, canFetch, providerId, providerType, baseUrl, apiKey, onError, t])

    const handleSelect = (model: string) => {
        onChange(model)
        setOpen(false)
        setSearch('')
    }

    const filteredModels = search
        ? models.filter((m) => m.toLowerCase().includes(search.toLowerCase()))
        : models

    return (
        <div className="model-select-wrapper" ref={wrapperRef}>
            <div className="model-select-input-row">
                <input
                    type="text"
                    className="modal-input model-select-input"
                    value={value}
                    onChange={(e) => onChange(e.target.value)}
                    placeholder={placeholder || 'gpt-4'}
                />
                <button
                    type="button"
                    className="model-select-fetch-btn"
                    onClick={hasFetched && models.length > 0 ? () => { setOpen(!open); setSearch('') } : handleFetch}
                    disabled={loading || !canFetch}
                    title={t('provider.fetchModels')}
                >
                    {loading ? (
                        <svg className="model-select-spinner" viewBox="0 0 24 24">
                            <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" strokeWidth="2.5" strokeDasharray="50 20" />
                        </svg>
                    ) : (
                        <svg viewBox="0 0 24 24">
                            <path d="M7 10l5 5 5-5z" />
                        </svg>
                    )}
                </button>
            </div>

            {open && models.length > 0 && (
                <div className="modal-select-menu model-select-dropdown">
                    {models.length > 5 && (
                        <div className="model-select-search-wrapper">
                            <input
                                ref={searchRef}
                                type="text"
                                className="model-select-search"
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                placeholder={t('provider.modelSearch')}
                            />
                        </div>
                    )}
                    <div className="model-select-list">
                        {filteredModels.length === 0 ? (
                            <div className="model-select-empty">{t('provider.noModelsFound')}</div>
                        ) : (
                            filteredModels.map((model) => {
                                const isActive = model === value
                                return (
                                    <button
                                        type="button"
                                        key={model}
                                        className={`modal-select-option${isActive ? ' active' : ''}`}
                                        onClick={() => handleSelect(model)}
                                    >
                                        <span className="model-select-option-text">{model}</span>
                                        {isActive && (
                                            <svg className="modal-select-check" viewBox="0 0 24 24">
                                                <path d="M9 16.17l-3.88-3.88L4 13.41 9 18.41 20 7.41 18.59 6l-9.59 10.17z" />
                                            </svg>
                                        )}
                                    </button>
                                )
                            })
                        )}
                    </div>
                </div>
            )}
        </div>
    )
}

export default ModelSelect
