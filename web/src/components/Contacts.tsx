import { useState, useEffect, useRef, useMemo } from 'react'
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
import { getPersonaCopy } from '../i18n/personas'
import { Avatar, EmptyState, Icon, Page, PageHeader } from './ui'
import './Contacts.css'

interface ContactsProps {
    onStartChat?: (sessionId: string, promptId: string) => void
    onEditPersona?: (promptId?: string) => void
    refreshToken?: number
}

const Contacts: React.FC<ContactsProps> = ({ onStartChat, onEditPersona, refreshToken }) => {
    const { locale } = useT()
    const copy = getPersonaCopy(locale).list
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [query, setQuery] = useState('')
    const [busy, setBusy] = useState(false)
    const busyRef = useRef(false)
    const requestRef = useRef(0)

    const dateFormat = useMemo(
        () => new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en', { month: 'short', day: 'numeric' }),
        [locale]
    )

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

    const formatDate = (value: string) => {
        const date = new Date(value)
        return Number.isNaN(date.getTime()) ? '' : copy.updated(dateFormat.format(date))
    }

    const normalizedQuery = query.trim().toLocaleLowerCase(locale)
    const filteredPrompts = prompts.filter((prompt) =>
        `${prompt.name} ${prompt.description || prompt.content}`.toLocaleLowerCase(locale).includes(normalizedQuery)
    )

    const renderBody = () => {
        if (loading && prompts.length === 0) {
            return <EmptyState role="status" title={copy.loading} />
        }
        if (error && prompts.length === 0) {
            return (
                <EmptyState
                    role="alert"
                    icon="alert"
                    title={error}
                    action={
                        <button type="button" className="ui-btn" disabled={loading} onClick={() => loadPrompts()}>
                            {copy.retry}
                        </button>
                    }
                />
            )
        }
        if (prompts.length === 0) {
            return (
                <EmptyState
                    icon="persona"
                    title={copy.empty}
                    description={copy.emptyHint}
                    action={
                        onEditPersona && (
                            <button type="button" className="ui-btn" onClick={() => onEditPersona()}>
                                <Icon name="plus" />
                                {copy.add}
                            </button>
                        )
                    }
                />
            )
        }
        if (filteredPrompts.length === 0) {
            return (
                <EmptyState
                    icon="search"
                    title={copy.noResults}
                    description={copy.noResultsHint}
                    action={
                        <button type="button" className="ui-btn" onClick={() => setQuery('')}>
                            {copy.clearSearch}
                        </button>
                    }
                />
            )
        }
        return (
            <ul className="contacts-grid">
                {filteredPrompts.map((prompt) => (
                    <li key={prompt.id} className="contact-card">
                        {onEditPersona && (
                            <button
                                type="button"
                                className="contact-card__open"
                                aria-label={copy.edit(prompt.name)}
                                onClick={() => onEditPersona(prompt.id)}
                            />
                        )}
                        <div className="contact-card__head">
                            <Avatar
                                className="contact-card__avatar"
                                name={prompt.name}
                                src={
                                    prompt.avatar
                                        ? appendQueryParam(
                                              getPromptAvatarUrl(prompt.id),
                                              't',
                                              new Date(prompt.updated_at).getTime()
                                          )
                                        : null
                                }
                            />
                            <h2 className="contact-card__name">{prompt.name}</h2>
                            <button
                                type="button"
                                className="ui-icon-btn contact-card__delete"
                                disabled={busy}
                                aria-label={copy.deleteLabel(prompt.name)}
                                title={copy.delete}
                                onClick={() => handleDelete(prompt)}
                            >
                                <Icon name="trash" />
                            </button>
                        </div>
                        <p className={`contact-card__desc${prompt.description ? '' : ' is-muted'}`}>
                            {prompt.description || prompt.content || copy.noDescription}
                        </p>
                        <div className="contact-card__foot">
                            <span className="contact-card__meta">{formatDate(prompt.updated_at)}</span>
                            {onStartChat && (
                                <button
                                    type="button"
                                    className="ui-btn ui-btn--sm contact-card__chat"
                                    disabled={busy}
                                    aria-label={copy.testChatLabel(prompt.name)}
                                    onClick={() => handleStartChat(prompt)}
                                >
                                    <Icon name="chat" />
                                    {copy.testChat}
                                </button>
                            )}
                        </div>
                    </li>
                ))}
            </ul>
        )
    }

    return (
        <Page wide className="contacts">
            <PageHeader
                title={copy.title}
                description={copy.subtitle}
                actions={
                    onEditPersona && (
                        <button type="button" className="ui-btn ui-btn--primary" onClick={() => onEditPersona()}>
                            <Icon name="plus" />
                            {copy.add}
                        </button>
                    )
                }
            />
            {prompts.length > 0 && (
                <div className="contacts-toolbar">
                    <label className="contacts-search">
                        <span className="ui-visually-hidden">{copy.searchLabel}</span>
                        <Icon name="search" size={16} className="contacts-search__icon" />
                        <input
                            type="search"
                            className="ui-input"
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
            )}
            {error && prompts.length > 0 && (
                <div className="contacts-alert" role="alert">
                    <Icon name="alert" size={16} />
                    <span>{error}</span>
                    <button
                        type="button"
                        className="ui-btn ui-btn--ghost ui-btn--sm"
                        disabled={loading}
                        onClick={() => loadPrompts()}
                    >
                        {copy.retry}
                    </button>
                </div>
            )}
            {renderBody()}
        </Page>
    )
}

export default Contacts
