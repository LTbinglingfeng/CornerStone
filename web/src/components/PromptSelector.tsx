import { useState, useEffect, useRef } from 'react'
import { gsap } from 'gsap'
import { getPrompts, getPromptAvatarUrl, appendQueryParam } from '../services/api'
import type { Prompt } from '../types/chat'
import './PromptSelector.css'

interface PromptSelectorProps {
    onSelect: (promptId: string, promptName: string) => void
    onClose: () => void
}

const PromptSelector: React.FC<PromptSelectorProps> = ({ onSelect, onClose }) => {
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const [loading, setLoading] = useState(true)
    const modalRef = useRef<HTMLDivElement>(null)
    const overlayRef = useRef<HTMLDivElement>(null)

    useEffect(() => {
        loadPrompts()
    }, [])

    useEffect(() => {
        if (overlayRef.current && modalRef.current) {
            gsap.fromTo(overlayRef.current, { opacity: 0 }, { opacity: 1, duration: 0.2 })
            gsap.fromTo(
                modalRef.current,
                { opacity: 0, y: 50 },
                { opacity: 1, y: 0, duration: 0.3, ease: 'power2.out' }
            )
        }
    }, [])

    const loadPrompts = async () => {
        setLoading(true)
        const data = await getPrompts()
        setPrompts(data)
        setLoading(false)
    }

    const handleClose = () => {
        if (overlayRef.current && modalRef.current) {
            gsap.to(modalRef.current, {
                opacity: 0,
                y: 50,
                duration: 0.2,
                ease: 'power2.in',
            })
            gsap.to(overlayRef.current, {
                opacity: 0,
                duration: 0.2,
                onComplete: onClose,
            })
        } else {
            onClose()
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
        <div className="prompt-selector-overlay" ref={overlayRef} onClick={handleClose}>
            <div className="prompt-selector-modal" ref={modalRef} onClick={(e) => e.stopPropagation()}>
                <div className="prompt-selector-header">
                    <h3>选择对话角色</h3>
                    <button className="prompt-selector-close" onClick={handleClose}>
                        <svg viewBox="0 0 24 24">
                            <path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" />
                        </svg>
                    </button>
                </div>

                <div className="prompt-selector-content">
                    {loading ? (
                        <div className="prompt-selector-loading">加载中...</div>
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
            </div>
        </div>
    )
}

export default PromptSelector
