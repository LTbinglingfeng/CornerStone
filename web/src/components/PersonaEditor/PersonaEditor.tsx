import { useCallback, useEffect, useRef, useState } from 'react'
import { motion } from 'motion/react'
import {
    getPrompt,
    createPrompt,
    updatePrompt,
    deletePrompt,
    uploadPromptAvatar,
    getPromptAvatarUrl,
    deletePromptAvatar,
    getErrorMessage,
} from '../../services/api'
import { useT } from '../../contexts/I18nContext'
import { useToast } from '../../contexts/ToastContext'
import { useConfirm } from '../../contexts/ConfirmContext'
import { memoryService, type MemoryStats } from '../../services/memoryService'
import { getPersonaCopy } from '../../i18n/personas'
import { drawerVariants } from '../../utils/motion'
import MemoryManager from '../MemoryManager'
import { Group, Icon, Row } from '../ui'
import PersonaHero from './PersonaHero'
import PersonaPromptSection from './PersonaPromptSection'
import PersonaMemorySection, { PERSONA_MEMORY_ROW_ID } from './PersonaMemorySection'
import './PersonaEditor.css'

interface PersonaEditorProps {
    promptId?: string // undefined = 新建, string = 编辑
    onBack: () => void
}

const PersonaEditor: React.FC<PersonaEditorProps> = ({ promptId, onBack }) => {
    const { t, locale } = useT()
    const editorCopy = getPersonaCopy(locale).editor
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const isEditing = !!promptId

    const [loading, setLoading] = useState(isEditing)
    const [saving, setSaving] = useState(false)
    const [formData, setFormData] = useState({ name: '', content: '', description: '' })
    const [avatarFile, setAvatarFile] = useState<File | null>(null)
    const [avatarPreview, setAvatarPreview] = useState<string | null>(null)
    const [hasExistingAvatar, setHasExistingAvatar] = useState(false)
    const [avatarDeleted, setAvatarDeleted] = useState(false)
    const [view, setView] = useState<'main' | 'memory'>('main')
    const [memoryStats, setMemoryStats] = useState<MemoryStats | null | undefined>(undefined)
    const titleRef = useRef<HTMLHeadingElement>(null)

    // 记忆摘要（只读），用于“记忆管理”行
    const loadMemoryStats = useCallback(async () => {
        if (!promptId) return
        try {
            setMemoryStats(await memoryService.getMemoryStats(promptId))
        } catch {
            setMemoryStats(null)
        }
    }, [promptId])

    useEffect(() => {
        void loadMemoryStats()
    }, [loadMemoryStats])

    // 加载已有角色数据
    useEffect(() => {
        if (!promptId) return

        const load = async () => {
            setLoading(true)
            try {
                const prompt = await getPrompt(promptId)
                if (prompt) {
                    setFormData({
                        name: prompt.name,
                        content: prompt.content,
                        description: prompt.description || '',
                    })
                    if (prompt.avatar) {
                        setAvatarPreview(getPromptAvatarUrl(promptId))
                        setHasExistingAvatar(true)
                    }
                }
            } catch (error) {
                showToast(getErrorMessage(error, t('persona.loadFailed')), 'error')
                onBack()
                return
            } finally {
                setLoading(false)
            }
        }
        void load()
    }, [onBack, promptId, showToast])

    // 保存
    const handleSave = useCallback(async () => {
        if (!formData.name.trim()) {
            showToast(t('persona.nameRequired'), 'error')
            return
        }
        if (!formData.content.trim()) {
            showToast(t('persona.promptRequired'), 'error')
            return
        }

        setSaving(true)
        try {
            if (isEditing && promptId) {
                await updatePrompt(promptId, formData)
                if (avatarDeleted && hasExistingAvatar) {
                    await deletePromptAvatar(promptId)
                }
                if (avatarFile) {
                    await uploadPromptAvatar(promptId, avatarFile)
                }
                showToast(t('persona.updateSuccess'), 'success')
            } else {
                const newPrompt = await createPrompt(formData)
                if (avatarFile) {
                    await uploadPromptAvatar(newPrompt.id, avatarFile)
                }
                showToast(t('persona.createSuccess'), 'success')
            }
            onBack()
        } catch (error) {
            showToast(
                getErrorMessage(error, isEditing ? t('persona.updateFailed') : t('persona.createFailed')),
                'error'
            )
        } finally {
            setSaving(false)
        }
    }, [avatarDeleted, avatarFile, formData, hasExistingAvatar, isEditing, onBack, promptId, showToast, t])

    // 删除角色
    const handleDelete = useCallback(async () => {
        if (!promptId) return
        const ok = await confirm({
            title: t('persona.deletePersona'),
            message: t('persona.deletePersonaConfirm', { name: formData.name }),
            confirmText: t('common.delete'),
            danger: true,
        })
        if (ok) {
            try {
                await deletePrompt(promptId)
                showToast(t('persona.deleteSuccess'), 'success')
                onBack()
            } catch (error) {
                showToast(getErrorMessage(error, t('persona.deleteFailed')), 'error')
            }
        }
    }, [confirm, formData.name, onBack, promptId, showToast, t])

    // 头像变更
    const handleAvatarChange = useCallback((file: File) => {
        setAvatarFile(file)
        setAvatarDeleted(false)
        const reader = new FileReader()
        reader.onloadend = () => {
            setAvatarPreview(reader.result as string)
        }
        reader.readAsDataURL(file)
    }, [])

    // 头像删除
    const handleAvatarDelete = useCallback(() => {
        setAvatarPreview(null)
        setAvatarFile(null)
        setAvatarDeleted(true)
    }, [])

    const openMemory = useCallback(() => setView('memory'), [])
    const closeMemory = useCallback(() => {
        setView('main')
        void loadMemoryStats()
    }, [loadMemoryStats])

    // Move focus with the view change so keyboard and screen-reader users keep their place.
    const firstViewRef = useRef(true)
    useEffect(() => {
        if (firstViewRef.current) {
            firstViewRef.current = false
            return
        }
        if (view === 'memory') {
            titleRef.current?.focus({ preventScroll: true })
        } else {
            document.getElementById(PERSONA_MEMORY_ROW_ID)?.focus({ preventScroll: true })
        }
    }, [view])

    const title = loading
        ? editorCopy.loading
        : view === 'memory'
          ? editorCopy.memoryTitle
          : isEditing
            ? editorCopy.editTitle
            : editorCopy.createTitle

    return (
        <motion.div
            className="persona-editor"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={drawerVariants}
        >
            <header className="persona-editor-header">
                <button
                    type="button"
                    className="ui-icon-btn persona-editor-back"
                    onClick={view === 'memory' ? closeMemory : onBack}
                    aria-label={t('common.back')}
                    title={t('common.back')}
                >
                    <Icon name="arrowLeft" size={20} />
                </button>
                <h1 className="persona-editor-title" ref={titleRef} tabIndex={-1}>
                    {title}
                    {view === 'memory' && formData.name.trim() && (
                        <span className="persona-editor-title__context">{formData.name.trim()}</span>
                    )}
                </h1>
                {!loading && (
                    <button
                        type="button"
                        className="save-btn ui-btn ui-btn--primary ui-btn--sm"
                        onClick={handleSave}
                        disabled={saving}
                    >
                        {saving ? t('common.saving') : t('common.save')}
                    </button>
                )}
            </header>

            {loading ? (
                <div className="persona-editor-content">
                    <div className="persona-editor-loading" role="status">
                        <span className="ui-spinner" aria-hidden="true" />
                        <span>{editorCopy.loading}</span>
                    </div>
                </div>
            ) : (
                <>
                    <div className="persona-editor-content" hidden={view !== 'main'}>
                        <div className="persona-editor-inner">
                            <PersonaHero
                                name={formData.name}
                                description={formData.description}
                                avatarUrl={avatarPreview}
                                onNameChange={(name) => setFormData((prev) => ({ ...prev, name }))}
                                onDescriptionChange={(description) => setFormData((prev) => ({ ...prev, description }))}
                                onAvatarChange={handleAvatarChange}
                                onAvatarDelete={handleAvatarDelete}
                            />

                            <PersonaPromptSection
                                content={formData.content}
                                personaName={formData.name}
                                onContentChange={(content) => setFormData((prev) => ({ ...prev, content }))}
                            />

                            {isEditing && promptId && <PersonaMemorySection stats={memoryStats} onOpen={openMemory} />}

                            {isEditing && (
                                <Group title={editorCopy.dangerZone} className="persona-danger">
                                    <Row
                                        label={editorCopy.deleteLabel}
                                        detail={editorCopy.deleteDetail}
                                        trailing={
                                            <button
                                                type="button"
                                                className="ui-btn ui-btn--danger ui-btn--sm delete-persona-btn"
                                                onClick={handleDelete}
                                            >
                                                {editorCopy.deleteButton}
                                            </button>
                                        }
                                    />
                                </Group>
                            )}
                        </div>
                    </div>
                    {isEditing && promptId && view === 'memory' && (
                        <div className="persona-editor-content persona-editor-content--memory">
                            <div className="persona-editor-inner">
                                <MemoryManager promptId={promptId} embedded />
                            </div>
                        </div>
                    )}
                </>
            )}
        </motion.div>
    )
}

export default PersonaEditor
