import { useState, useEffect } from 'react'
import { motion } from 'motion/react'
import { getPrompts, getPromptAvatarUrl, appendQueryParam, getErrorMessage } from '../services/api'
import type { Prompt } from '../types/chat'
import { centerModalVariants, overlayVariants } from '../utils/motion'
import './PromptSelector.css'

interface PromptSelectorProps {
    onSelect: (promptId: string, promptName: string) => void
    onClose: () => void
}

const PromptSelector: React.FC<PromptSelectorProps> = ({ onSelect, onClose }) => {
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
            setError(getErrorMessage(error, '加载提示词失败，请重试'))
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

    return (
        <motion.div
            className="prompt-selector-overlay"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={overlayVariants}
            onClick={onClose}
        >
            <motion.div
                className="prompt-selector-modal"
                initial="hidden"
                animate="visible"
                exit="hidden"
                variants={centerModalVariants}
                onClick={(e) => e.stopPropagation()}
            >
                <div className="prompt-selector-header">
                    <h3>选择对话角色</h3>
                    <button className="prompt-selector-close" onClick={onClose}>
                        <svg viewBox="0 0 24 24">
                            <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                        </svg>
                    </button>
                </div>

                <div className="prompt-selector-content">
                    {loading ? (
                        <div className="prompt-selector-loading">加载中...</div>
                    ) : error ? (
                        <div className="prompt-selector-empty">
                            <p>{error}</p>
                            <p className="hint">请稍后重试</p>
                        </div>
                    ) : prompts.length === 0 ? (
                        <div className="prompt-selector-empty">
                            <p>暂无提示词模板</p>
                            <p className="hint">请先在通讯录中创建提示词</p>
                        </div>
                    ) : (
                        <div className="prompt-selector-list">
                            {prompts.map((prompt) => (
                                <div
                                    key={prompt.id}
                                    className="prompt-selector-item"
                                    onClick={() => handleSelect(prompt)}
                                >
                                    <div className="prompt-selector-avatar">
                                        {getAvatarUrl(prompt) ? (
                                            <img src={getAvatarUrl(prompt)!} alt={prompt.name} />
                                        ) : (
                                            <div className="avatar-placeholder">
                                                {prompt.name.charAt(0).toUpperCase()}
                                            </div>
                                        )}
                                    </div>
                                    <div className="prompt-selector-info">
                                        <div className="prompt-selector-name">{prompt.name}</div>
                                        <div className="prompt-selector-desc">
                                            {prompt.description ||
                                                prompt.content.substring(0, 40) +
                                                    (prompt.content.length > 40 ? '...' : '')}
                                        </div>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            </motion.div>
        </motion.div>
    )
}

export default PromptSelector
