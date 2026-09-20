import { useEffect, useMemo, useRef, useState } from 'react'
import type { ChatSession, Prompt } from '../types/chat'
import {
    appendQueryParam,
    deleteSession,
    getErrorMessage,
    getPromptAvatarUrl,
    getPrompts,
    getSessions,
} from '../services/api'
import { useT } from '../contexts/I18nContext'
import { useToast } from '../contexts/ToastContext'
import { useConfirm } from '../contexts/ConfirmContext'
import { getManagementCopy } from '../i18n/archive'
import './ChatList.css'

interface ChatListProps {
    onSelectSession: (id: string, promptId?: string) => void
    searchQuery?: string
    refreshToken?: number
}

interface ArchivedSession {
    session: ChatSession
    prompt?: Prompt
    personaName: string
}

const NO_PERSONA_FILTER = '__none__'

const ChatList: React.FC<ChatListProps> = ({ onSelectSession, searchQuery = '', refreshToken }) => {
    const { locale, t } = useT()
    const copy = getManagementCopy(locale).archive
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const [sessions, setSessions] = useState<ArchivedSession[]>([])
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [personaFilter, setPersonaFilter] = useState('')
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [deletingId, setDeletingId] = useState('')
    const requestRef = useRef(0)
    const lastRefreshTokenRef = useRef<number | undefined>(undefined)

    const loadData = async (showLoading = true) => {
        const request = ++requestRef.current
        if (showLoading) setLoading(true)
        try {
            const [sessionData, promptData] = await Promise.all([getSessions(), getPrompts()])
            if (request !== requestRef.current) return
            const promptMap = new Map(promptData.map((prompt) => [prompt.id, prompt]))
            const archive = sessionData
                .map((session) => {
                    const prompt = session.prompt_id ? promptMap.get(session.prompt_id) : undefined
                    return {
                        session,
                        prompt,
                        personaName: prompt?.name || session.prompt_name || copy.noPersona,
                    }
                })
                .sort((a, b) => new Date(b.session.updated_at).getTime() - new Date(a.session.updated_at).getTime())

            setPrompts(promptData)
            setSessions(archive)
            setError('')
        } catch (loadError) {
            if (request !== requestRef.current) return
            setError(getErrorMessage(loadError, copy.loadError))
        } finally {
            if (request === requestRef.current) setLoading(false)
        }
    }

    useEffect(() => {
        if (typeof refreshToken === 'number') lastRefreshTokenRef.current = refreshToken
        loadData()
    }, [])

    useEffect(() => {
        if (typeof refreshToken !== 'number' || lastRefreshTokenRef.current === refreshToken) return
        lastRefreshTokenRef.current = refreshToken
        loadData(false)
    }, [refreshToken])

    const filteredSessions = useMemo(() => {
        const query = searchQuery.trim().toLocaleLowerCase(locale)
        return sessions.filter(({ session, personaName }) => {
            const matchesQuery = !query || `${session.title} ${personaName}`.toLocaleLowerCase(locale).includes(query)
            const matchesPersona =
                !personaFilter ||
                (personaFilter === NO_PERSONA_FILTER
                    ? !session.prompt_id || !prompts.some((prompt) => prompt.id === session.prompt_id)
                    : session.prompt_id === personaFilter)
            return matchesQuery && matchesPersona
        })
    }, [locale, personaFilter, prompts, searchQuery, sessions])

    const formatDate = (value: string) => {
        const date = new Date(value)
        if (Number.isNaN(date.getTime())) return value
        return new Intl.DateTimeFormat(locale === 'en' ? 'en-US' : 'zh-CN', {
            year: 'numeric',
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
        }).format(date)
    }

    const getAvatarUrl = (prompt?: Prompt) => {
        if (!prompt?.avatar) return null
        return appendQueryParam(getPromptAvatarUrl(prompt.id), 't', new Date(prompt.updated_at).getTime())
    }

    const handleDelete = async (item: ArchivedSession) => {
        const accepted = await confirm({
            title: copy.deleteTitle,
            message: copy.deleteMessage(item.session.title),
            confirmText: copy.delete,
            danger: true,
        })
        if (!accepted) return

        setDeletingId(item.session.id)
        try {
            const success = await deleteSession(item.session.id)
            if (!success) throw new Error(copy.deleteFailed)
            setSessions((current) => current.filter(({ session }) => session.id !== item.session.id))
        } catch (deleteError) {
            showToast(getErrorMessage(deleteError, copy.deleteFailed), 'error')
        } finally {
            setDeletingId('')
        }
    }

    const hasFilters = Boolean(searchQuery.trim() || personaFilter)

    return (
        <section className="archive" aria-label={copy.conversations}>
            <div className="archive-toolbar">
                <p className="archive-count" aria-live="polite">
                    {copy.showing} <strong>{filteredSessions.length}</strong> {copy.of} {sessions.length}{' '}
                    {copy.conversations}
                </p>
                <label className="archive-filter">
                    <span>{copy.filterLabel}</span>
                    <select value={personaFilter} onChange={(event) => setPersonaFilter(event.target.value)}>
                        <option value="">{copy.allPersonas}</option>
                        {prompts.map((prompt) => (
                            <option key={prompt.id} value={prompt.id}>
                                {prompt.name}
                            </option>
                        ))}
                        {personaFilter &&
                            personaFilter !== NO_PERSONA_FILTER &&
                            !prompts.some((prompt) => prompt.id === personaFilter) && (
                                <option value={personaFilter}>{copy.deletedPersona}</option>
                            )}
                        <option value={NO_PERSONA_FILTER}>{copy.noPersona}</option>
                    </select>
                </label>
            </div>

            {error && (
                <div className="archive-error" role="alert">
                    <p>{error}</p>
                    <button type="button" disabled={loading} onClick={() => loadData()}>
                        {copy.retry}
                    </button>
                </div>
            )}
            {loading && sessions.length === 0 ? (
                <div className="archive-state" role="status">
                    <span className="archive-spinner" aria-hidden="true" />
                    {t('common.loading')}
                </div>
            ) : error && sessions.length === 0 ? null : filteredSessions.length === 0 ? (
                <div className="archive-state">
                    <p>{hasFilters ? copy.noResults : copy.empty}</p>
                </div>
            ) : (
                <ul className="archive-list">
                    {filteredSessions.map((item) => {
                        const avatarUrl = getAvatarUrl(item.prompt)
                        return (
                            <li key={item.session.id} className="archive-card">
                                <div className="archive-avatar" aria-hidden="true">
                                    {avatarUrl ? (
                                        <img src={avatarUrl} alt="" />
                                    ) : (
                                        <span>{item.personaName.charAt(0).toUpperCase() || '?'}</span>
                                    )}
                                </div>
                                <div className="archive-details">
                                    <div className="archive-title-row">
                                        <h2>{item.session.title}</h2>
                                        <span className="archive-persona">{item.personaName}</span>
                                    </div>
                                    <dl className="archive-dates">
                                        <div>
                                            <dt>{copy.updated}</dt>
                                            <dd>{formatDate(item.session.updated_at)}</dd>
                                        </div>
                                        <div>
                                            <dt>{copy.created}</dt>
                                            <dd>{formatDate(item.session.created_at)}</dd>
                                        </div>
                                    </dl>
                                </div>
                                <div className="archive-actions">
                                    <button
                                        type="button"
                                        className="archive-open"
                                        aria-label={`${copy.open}: ${item.session.title}`}
                                        onClick={() => onSelectSession(item.session.id, item.session.prompt_id)}
                                    >
                                        {copy.open}
                                    </button>
                                    <button
                                        type="button"
                                        className="archive-delete"
                                        disabled={Boolean(deletingId)}
                                        onClick={() => handleDelete(item)}
                                        aria-label={`${copy.delete}: ${item.session.title}`}
                                    >
                                        {copy.delete}
                                    </button>
                                </div>
                            </li>
                        )
                    })}
                </ul>
            )}
        </section>
    )
}

export default ChatList
