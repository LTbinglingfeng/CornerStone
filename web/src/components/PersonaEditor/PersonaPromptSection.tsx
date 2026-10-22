import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { AnimatePresence } from 'motion/react'
import { useT } from '../../contexts/I18nContext'
import { getPersonaCopy } from '../../i18n/personas'
import { fullscreenVariants } from '../../utils/motion'
import { ModalOverlay } from '../ModalOverlay'
import { Icon } from '../ui'

interface PersonaPromptSectionProps {
    content: string
    personaName?: string
    onContentChange: (content: string) => void
}

interface Selection {
    start: number
    end: number
}

const readSelection = (el: HTMLTextAreaElement | null): Selection | null =>
    el ? { start: el.selectionStart, end: el.selectionEnd } : null

interface FullscreenTextareaProps {
    labelledBy: string
    value: string
    placeholder: string
    initialSelection: Selection | null
    onChange: (value: string) => void
    onSelectionChange: (sel: Selection) => void
}

/** Focus runs in a passive effect so it lands after the dialog's showModal() focus pass. */
const FullscreenTextarea: React.FC<FullscreenTextareaProps> = ({
    labelledBy,
    value,
    placeholder,
    initialSelection,
    onChange,
    onSelectionChange,
}) => {
    const ref = useRef<HTMLTextAreaElement>(null)
    const initialRef = useRef(initialSelection)

    useEffect(() => {
        const el = ref.current
        if (!el) return
        const sel = initialRef.current ?? { start: el.value.length, end: el.value.length }
        el.focus({ preventScroll: true })
        el.setSelectionRange(sel.start, sel.end)
        // Keep the caret roughly centred instead of jumping to the top of a long prompt.
        const linesBefore = el.value.slice(0, sel.start).split('\n').length
        const lineHeight = parseFloat(getComputedStyle(el).lineHeight) || 24
        el.scrollTop = Math.max(0, linesBefore * lineHeight - el.clientHeight / 2)
    }, [])

    const report = (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
        const sel = readSelection(e.currentTarget)
        if (sel) onSelectionChange(sel)
    }

    return (
        <textarea
            ref={ref}
            className="persona-prompt-fullscreen__textarea"
            aria-labelledby={labelledBy}
            value={value}
            placeholder={placeholder}
            onChange={(e) => {
                onChange(e.target.value)
                report(e)
            }}
            onSelect={report}
        />
    )
}

/**
 * Prompt editor with a fixed-height inline field (so memory and other options stay reachable)
 * and a full-screen mode for long prompts. Both fields edit the same value; the caret carries over.
 */
const PersonaPromptSection: React.FC<PersonaPromptSectionProps> = ({ content, personaName, onContentChange }) => {
    const { locale } = useT()
    const copy = getPersonaCopy(locale).editor
    const promptId = useId()
    const hintId = useId()
    const fullTitleId = useId()
    const inlineRef = useRef<HTMLTextAreaElement>(null)
    const selectionRef = useRef<Selection | null>(null)
    const [expanded, setExpanded] = useState(false)

    const open = useCallback(() => {
        selectionRef.current = readSelection(inlineRef.current)
        setExpanded(true)
    }, [])

    const close = useCallback(() => setExpanded(false), [])

    const handleFullSelection = useCallback((sel: Selection) => {
        selectionRef.current = sel
    }, [])

    const handleExitComplete = () => {
        const sel = selectionRef.current
        const el = inlineRef.current
        if (el && sel) el.setSelectionRange(sel.start, sel.end)
    }

    const count = copy.characters(content.length)
    const name = personaName?.trim()

    return (
        <section className="ui-group persona-prompt">
            <div className="ui-group__head">
                <h2 className="ui-group__title">
                    <label htmlFor={promptId}>{copy.prompt}</label>
                </h2>
                <div className="persona-prompt__meta">
                    <span className="persona-prompt__count">{count}</span>
                    <button
                        type="button"
                        className="ui-icon-btn persona-prompt__expand"
                        onClick={open}
                        aria-label={copy.expandPrompt}
                        title={copy.expandPrompt}
                        aria-haspopup="dialog"
                    >
                        <Icon name="maximize" size={16} />
                    </button>
                </div>
            </div>
            <textarea
                id={promptId}
                ref={inlineRef}
                required
                className="prompt-textarea"
                value={content}
                onChange={(e) => onContentChange(e.target.value)}
                placeholder={copy.promptPlaceholder}
                aria-describedby={hintId}
                rows={14}
            />
            <p id={hintId} className="ui-group__foot">
                {copy.promptHint}
            </p>

            <AnimatePresence onExitComplete={handleExitComplete}>
                {expanded && (
                    <ModalOverlay
                        className="persona-prompt-fullscreen"
                        aria-label={copy.prompt}
                        aria-labelledby={fullTitleId}
                        initial="hidden"
                        animate="visible"
                        exit="hidden"
                        variants={fullscreenVariants}
                        onDismiss={close}
                    >
                        <div className="persona-prompt-fullscreen__panel">
                            <header className="persona-prompt-fullscreen__header">
                                <h2 id={fullTitleId} className="persona-prompt-fullscreen__title">
                                    {copy.prompt}
                                    {name && <span className="persona-prompt-fullscreen__context">{name}</span>}
                                </h2>
                                <span className="persona-prompt__count">{count}</span>
                                <button
                                    type="button"
                                    className="ui-icon-btn"
                                    onClick={close}
                                    aria-label={copy.collapsePrompt}
                                    title={copy.collapsePrompt}
                                >
                                    <Icon name="minimize" size={18} />
                                </button>
                            </header>
                            <div className="persona-prompt-fullscreen__body">
                                <FullscreenTextarea
                                    labelledBy={fullTitleId}
                                    value={content}
                                    placeholder={copy.promptPlaceholder}
                                    initialSelection={selectionRef.current}
                                    onChange={onContentChange}
                                    onSelectionChange={handleFullSelection}
                                />
                            </div>
                        </div>
                    </ModalOverlay>
                )}
            </AnimatePresence>
        </section>
    )
}

export default PersonaPromptSection
