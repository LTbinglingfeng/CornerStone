import { useState, useEffect } from 'react'
import { motion } from 'motion/react'
import { getPrompts, getPromptAvatarUrl, appendQueryParam, getErrorMessage } from '../services/api'
import { useT } from '../contexts/I18nContext'
import type { Prompt } from '../types/chat'
import { centerModalVariants, overlayVariants } from '../utils/motion'
import { ModalOverlay } from './ModalOverlay'
import { useNavigate } from 'react-router-dom'
import { getManagementCopy } from '../i18n/archive'
import { tabRoutes } from '../utils/routes'
import { Avatar, EmptyState, Icon } from './ui'
import './PromptSelector.css'

interface PromptSelectorProps {
    onSelect: (promptId: string, promptName: string) => void
    onClose: () => void
}

const PromptSelector: React.FC<PromptSelectorProps> = ({ onSelect, onClose }) => {
    const { t, locale } = useT()
    const copy = getManagementCopy(locale).conversation
    const navigate = useNavigate()
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')

    useEffect(() => {
        loadPrompts()
    }, [])

    const loadPrompts = async () => {
        setLoading(true)
        try {
            const data = await getPrompts()
            setPrompts(data)
            setError('')
        } catch (error) {
            setPrompts([])
            setError(getErrorMessage(error, t('promptSelector.loadFailed')))
        } finally {
            setLoading(false)
        }
    }

    const handleSelect = (prompt: Prompt) => {
        onSelect(prompt.id, prompt.name)
    }

    const getAvatarUrl = (prompt: Prompt) => {
        if (prompt.avatar) {
            return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
        }
        return null
    }

    const goToPersonas = () => {
        onClose()
        navigate(tabRoutes.contacts)
    }

    return (
        <ModalOverlay
            aria-label={copy.pickPersona}
            onDismiss={onClose}
            className="prompt-modal-overlay prompt-selector-overlay"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={overlayVariants}
        >
            <motion.div className="prompt-modal-card prompt-selector-modal" variants={centerModalVariants}>
                <div className="prompt-modal-header prompt-selector-header">
                    <div>
                        <h3>{copy.pickPersona}</h3>
                        <p className="prompt-selector-subtitle">{copy.pickPersonaHint}</p>
                    </div>
                    <button
                        type="button"
                        className="prompt-modal-close"
                        onClick={onClose}
                        aria-label={t('common.close')}
                    >
                        <Icon name="x" />
                    </button>
                </div>

                <div className="prompt-selector-content">
                    {loading ? (
                        <div className="prompt-selector-loading" role="status">
                            <span className="ui-spinner" aria-hidden="true" />
                            <span className="ui-visually-hidden">{t('common.loading')}</span>
                        </div>
                    ) : error ? (
                        <EmptyState
                            icon="alert"
                            role="alert"
                            title={copy.loadFailed}
                            description={error}
                            action={
                                <button type="button" className="ui-btn ui-btn--sm" onClick={loadPrompts}>
                                    {getManagementCopy(locale).archive.retry}
                                </button>
                            }
                        />
                    ) : prompts.length === 0 ? (
                        <EmptyState
                            icon="persona"
                            title={copy.noPersonas}
                            description={copy.noPersonasHint}
                            action={
                                <button type="button" className="ui-btn ui-btn--primary" onClick={goToPersonas}>
                                    {copy.createPersona}
                                    <Icon name="arrowRight" size={16} />
                                </button>
                            }
                        />
                    ) : (
                        <ul className="prompt-selector-list">
                            {prompts.map((prompt) => {
                                const description = prompt.description || prompt.content
                                return (
                                    <li key={prompt.id}>
                                        <button
                                            type="button"
                                            className="prompt-selector-item"
                                            onClick={() => handleSelect(prompt)}
                                        >
                                            <Avatar src={getAvatarUrl(prompt)} name={prompt.name} />
                                            <span className="prompt-selector-info">
                                                <span className="prompt-selector-name">{prompt.name}</span>
                                                {description && (
                                                    <span className="prompt-selector-desc">{description}</span>
                                                )}
                                            </span>
                                            <Icon name="chevronRight" size={16} className="prompt-selector-chevron" />
                                        </button>
                                    </li>
                                )
                            })}
                        </ul>
                    )}
                </div>
            </motion.div>
        </ModalOverlay>
    )
}

export default PromptSelector
