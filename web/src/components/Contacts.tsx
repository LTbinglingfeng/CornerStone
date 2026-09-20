import { useState, useEffect, useRef } from 'react'
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
import { getManagementCopy } from '../i18n/archive'
import './Contacts.css'

interface ContactsProps {
    onStartChat?: (sessionId: string, promptId: string) => void
    onEditPersona?: (promptId?: string) => void
    refreshToken?: number
}

const Contacts: React.FC<ContactsProps> = ({ onStartChat, onEditPersona, refreshToken }) => {
    const { t, locale } = useT()
    const copy = getManagementCopy(locale).personas
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [query, setQuery] = useState('')
    const [busy, setBusy] = useState(false)
    const busyRef = useRef(false)
    const requestRef = useRef(0)

    const loadPrompts = async () => {
        const request = ++requestRef.current
        setLoading(true)
        try {
            const data = await getPrompts()
            if (request !== requestRef.current) return
            setPrompts(data)
            setError('')
        } catch (error) {
            if (request !== requestRef.current) return
            setError(getErrorMessage(error, copy.loadError))
        } finally {
            if (request === requestRef.current) setLoading(false)
        }
    }

    useEffect(() => {
        loadPrompts()
        return () => {
            requestRef.current++
        }
    }, [refreshToken])

    const handleDelete = async (prompt: Prompt) => {
        if (busyRef.current) return
        busyRef.current = true
        setBusy(true)
        try {
            const ok = await confirm({
                title: copy.deleteTitle,
                message: copy.deleteMessage(prompt.name),
                confirmText: copy.delete,
                danger: true,
            })
            if (!ok) return
            await deletePrompt(prompt.id)
            // Invalidate any older refresh so it cannot restore a deleted card.
            requestRef.current++
            setLoading(false)
            setPrompts((current) => current.filter((item) => item.id !== prompt.id))
            showToast(copy.deleteSuccess, 'success')
        } catch (error) {
            showToast(getErrorMessage(error, copy.deleteFailed), 'error')
        } finally {
            busyRef.current = false
            setBusy(false)
        }
    }

    const handleStartChat = async (prompt: Prompt) => {
        if (!onStartChat || busyRef.current) return
        busyRef.current = true
        setBusy(true)
        try {
            const session = await createSession(prompt.name, prompt.id)
            if (!session) throw new Error(copy.chatFailed)
            onStartChat(session.id, prompt.id)
        } catch (error) {
            showToast(getErrorMessage(error, copy.chatFailed), 'error')
        } finally {
            busyRef.current = false
            setBusy(false)
        }
    }

    const normalizedQuery = query.trim().toLocaleLowerCase(locale)
    const filteredPrompts = prompts.filter((prompt) =>
        `${prompt.name} ${prompt.description || prompt.content}`.toLocaleLowerCase(locale).includes(normalizedQuery)
    )

    return (
        <section className="contacts" aria-labelledby="personas-heading">
            <header className="contacts-header">
                <div>
                    <h1 id="personas-heading">{copy.title}</h1>
                    <p>{copy.subtitle}</p>
                </div>
                {onEditPersona && (
                    <button type="button" className="contacts-primary" onClick={() => onEditPersona()}>
                        {copy.add}
                    </button>
                )}
            </header>
            <div className="contacts-toolbar">
                <label className="contacts-search">
                    <span>{copy.searchLabel}</span>
                    <input
                        type="search"
                        value={query}
                        onChange={(event) => setQuery(event.target.value)}
                        placeholder={copy.searchPlaceholder}
                    />
                </label>
                <p className="contacts-count" aria-live="polite">
                    {normalizedQuery
                        ? copy.results(filteredPrompts.length, prompts.length)
                        : copy.count(prompts.length)}
                </p>
            </div>
            <div className="contacts-content">
                {error && (
                    <div className="contacts-error" role="alert">
                        <p>{error}</p>
                        <button type="button" disabled={loading} onClick={() => loadPrompts()}>
                            {copy.retry}
                        </button>
                    </div>
                )}
                {loading && prompts.length === 0 ? (
                    <div className="contacts-empty" role="status">
                        {t('common.loading')}
                    </div>
                ) : error && prompts.length === 0 ? null : filteredPrompts.length === 0 ? (
                    <div className="contacts-empty">
                        <p>{normalizedQuery ? copy.noResults : copy.empty}</p>
                        {!normalizedQuery && <p>{copy.emptyHint}</p>}
                    </div>
                ) : (
                    <ul className="contacts-list">
                        {filteredPrompts.map((prompt) => (
                            <li key={prompt.id} className="contact-card">
                                <div className="contact-summary">
                                    <div className="contact-avatar" aria-hidden="true">
                                        {prompt.avatar ? (
                                            <img
                                                src={appendQueryParam(
                                                    getPromptAvatarUrl(prompt.id),
                                                    't',
                                                    new Date(prompt.updated_at).getTime()
                                                )}
                                                alt=""
                                            />
                                        ) : (
                                            prompt.name.charAt(0).toUpperCase() || '?'
                                        )}
                                    </div>
                                    <h2>{prompt.name}</h2>
                                </div>
                                <p className="contact-description">
                                    {prompt.description || prompt.content || copy.noDescription}
                                </p>
                                <div className="contact-actions">
                                    {onEditPersona && (
                                        <button
                                            type="button"
                                            className="contacts-primary"
                                            aria-label={`${copy.edit}: ${prompt.name}`}
                                            onClick={() => onEditPersona(prompt.id)}
                                        >
                                            {copy.edit}
                                        </button>
                                    )}
                                    {onStartChat && (
                                        <button
                                            type="button"
                                            disabled={busy}
                                            aria-label={`${copy.testChat}: ${prompt.name}`}
                                            onClick={() => handleStartChat(prompt)}
                                        >
                                            {copy.testChat}
                                        </button>
                                    )}
                                    <button
                                        type="button"
                                        className="contact-delete"
                                        disabled={busy}
                                        aria-label={`${copy.delete}: ${prompt.name}`}
                                        onClick={() => handleDelete(prompt)}
                                    >
                                        {copy.delete}
                                    </button>
                                </div>
                            </li>
                        ))}
                    </ul>
                )}
            </div>
        </section>
    )
}

export default Contacts
