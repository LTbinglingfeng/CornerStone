import { useCallback, useEffect, useState } from 'react'
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
import { drawerVariants } from '../../utils/motion'
import PersonaHero from './PersonaHero'
import PersonaPromptSection from './PersonaPromptSection'
import PersonaMemorySection from './PersonaMemorySection'
import './PersonaEditor.css'

interface PersonaEditorProps {
    promptId?: string // undefined = 新建, string = 编辑
    onBack: () => void
}

const PersonaEditor: React.FC<PersonaEditorProps> = ({ promptId, onBack }) => {
    const { t } = useT()
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
    const [memoryExpanded, setMemoryExpanded] = useState(false)
    const [memoryCount, setMemoryCount] = useState(0)

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

    if (loading) {
        return (
            <motion.div
                className="persona-editor"
                initial="hidden"
                animate="visible"
                exit="hidden"
                variants={drawerVariants}
            >
                <div className="persona-editor-header">
                    <div className="header-left">
                        <button className="back-btn" onClick={onBack}>
                            <svg viewBox="0 0 24 24">
                                <path d="M15.41 7.41L14 6l-6 6 6 6 1.41-1.41L10.83 12z" />
                            </svg>
                            {t('common.back')}
                        </button>
                    </div>
                    <div className="header-title">{t('common.loading')}</div>
                    <div className="header-right" />
                </div>
            </motion.div>
        )
    }

    return (
        <motion.div
            className="persona-editor"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={drawerVariants}
        >
            {/* Header */}
            <div className="persona-editor-header">
                <div className="header-left">
                    <button className="back-btn" onClick={onBack}>
                        <svg viewBox="0 0 24 24">
                            <path d="M15.41 7.41L14 6l-6 6 6 6 1.41-1.41L10.83 12z" />
                        </svg>
                        {t('common.back')}
                    </button>
                </div>
                <div className="header-title">{isEditing ? t('persona.editTitle') : t('persona.createTitle')}</div>
                <div className="header-right">
                    <button className="save-btn" onClick={handleSave} disabled={saving}>
                        {saving ? t('common.saving') : t('common.save')}
                    </button>
                </div>
            </div>

            {/* 滚动内容 */}
            <div className="persona-editor-content">
                {/* Hero: 头像 + 名称 + 描述 */}
                <PersonaHero
                    name={formData.name}
                    description={formData.description}
                    avatarUrl={avatarPreview}
                    onNameChange={(name) => setFormData((prev) => ({ ...prev, name }))}
                    onDescriptionChange={(description) => setFormData((prev) => ({ ...prev, description }))}
                    onAvatarChange={handleAvatarChange}
                    onAvatarDelete={handleAvatarDelete}
                />

                {/* 系统提示词 */}
                <PersonaPromptSection
                    content={formData.content}
                    onContentChange={(content) => setFormData((prev) => ({ ...prev, content }))}
                />

                {/* 记忆管理（仅编辑时显示） */}
                {isEditing && promptId && (
                    <PersonaMemorySection
                        promptId={promptId}
                        expanded={memoryExpanded}
                        onToggle={() => setMemoryExpanded((prev) => !prev)}
                        memoryCount={memoryCount}
                        onMemoryCountChange={setMemoryCount}
                    />
                )}

                {/* 删除区域（仅编辑时显示） */}
                {isEditing && (
                    <div className="danger-zone">
                        <button className="delete-persona-btn" onClick={handleDelete}>
                            {t('persona.deleteThisPersona')}
                        </button>
                    </div>
                )}
            </div>
        </motion.div>
    )
}

export default PersonaEditor
