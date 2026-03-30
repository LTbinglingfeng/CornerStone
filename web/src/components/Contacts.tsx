import { useState, useEffect } from 'react'
import {
    getPrompts,
    deletePrompt,
    getPromptAvatarUrl,
    createSession,
    appendQueryParam,
    getErrorMessage,
} from '../services/api'
import type { Prompt } from '../types/chat'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import './Contacts.css'

interface ContactsProps {
    onStartChat?: (sessionId: string, promptId: string) => void
    onEditPersona?: (promptId?: string) => void
    refreshToken?: number
}

const Contacts: React.FC<ContactsProps> = ({ onStartChat, onEditPersona, refreshToken }) => {
    const { t } = useT()
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')

    const loadPrompts = async () => {
        setLoading(true)
        try {
            const data = await getPrompts()
            setPrompts(data)
            setError('')
        } catch (error) {
            setPrompts([])
            setError(getErrorMessage(error, t('contacts.loadFailed')))
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        loadPrompts()
    }, [refreshToken])

    const handleDelete = async (prompt: Prompt) => {
        const ok = await confirm({
            title: t('contacts.deletePrompt'),
            message: t('contacts.deletePromptConfirm', { name: prompt.name }),
            confirmText: t('common.delete'),
            danger: true,
        })
        if (ok) {
            try {
                await deletePrompt(prompt.id)
                showToast(t('persona.deleteSuccess'), 'success')
                await loadPrompts()
            } catch (error) {
                showToast(getErrorMessage(error, t('persona.deleteFailed')), 'error')
            }
        }
    }

    const handleStartChat = async (prompt: Prompt) => {
        if (!onStartChat) return
        const session = await createSession(prompt.name, prompt.id)
        if (session) {
            onStartChat(session.id, prompt.id)
            return
        }
        showToast(t('chat.createSessionFailed'), 'error')
    }

    const getAvatarUrl = (prompt: Prompt) => {
        if (prompt.avatar) {
            return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
        }
        return null
    }

    return (
        <div className="contacts">
            <div className="contacts-header">
                <div style={{ width: 44 }}></div>
                <div className="contacts-title">{t('contacts.title')}</div>
                <button className="add-button" onClick={() => onEditPersona?.()}>
                    <svg viewBox="0 0 24 24">
                        <path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z" />
                    </svg>
                </button>
            </div>

            <div className="contacts-content">
                {loading ? (
                    <div className="contacts-loading">{t('common.loading')}</div>
                ) : error ? (
                    <div className="contacts-empty">
                        <p>{error}</p>
                    </div>
                ) : prompts.length === 0 ? (
                    <div className="contacts-empty">
                        <div className="empty-icon">
                            <svg viewBox="0 0 24 24">
                                <path d="M19 3h-4.18C14.4 1.84 13.3 1 12 1c-1.3 0-2.4.84-2.82 2H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-7 0c.55 0 1 .45 1 1s-.45 1-1 1-1-.45-1-1 .45-1 1-1zm0 4c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm6 12H6v-1.4c0-2 4-3.1 6-3.1s6 1.1 6 3.1V19z" />
                            </svg>
                        </div>
                        <p>{t('contacts.noPrompts')}</p>
                        <p className="empty-hint">{t('contacts.createPromptHint')}</p>
                    </div>
                ) : (
                    <div className="contacts-list">
                        {prompts.map((prompt) => (
                            <div key={prompt.id} className="contact-item" onClick={() => onEditPersona?.(prompt.id)}>
                                <div className="contact-avatar">
                                    {getAvatarUrl(prompt) ? (
                                        <img src={getAvatarUrl(prompt)!} alt={prompt.name} />
                                    ) : (
                                        <div className="avatar-placeholder">{prompt.name.charAt(0).toUpperCase()}</div>
                                    )}
                                </div>
                                <div className="contact-info">
                                    <div className="contact-name">{prompt.name}</div>
                                    <div className="contact-desc">
                                        {prompt.description ||
                                            prompt.content.substring(0, 50) + (prompt.content.length > 50 ? '...' : '')}
                                    </div>
                                </div>
                                <div className="contact-actions">
                                    {onStartChat && (
                                        <button
                                            className="contact-chat"
                                            onClick={(e) => {
                                                e.stopPropagation()
                                                handleStartChat(prompt)
                                            }}
                                        >
                                            <svg viewBox="0 0 24 24">
                                                <path d="M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z" />
                                            </svg>
                                        </button>
                                    )}
                                    <button
                                        className="contact-delete"
                                        onClick={(e) => {
                                            e.stopPropagation()
                                            handleDelete(prompt)
                                        }}
                                    >
                                        <svg viewBox="0 0 24 24">
                                            <path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z" />
                                        </svg>
                                    </button>
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    )
}

export default Contacts
