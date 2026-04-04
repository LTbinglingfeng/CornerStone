import { useEffect, useMemo, useState } from 'react'
import { motion } from 'motion/react'
import { getErrorMessage, getProviders, updateConfig } from '../services/api'
import {
    countEnabledToolToggles,
    createDefaultToolToggles,
    normalizeToolToggles,
    TOOL_CONTROL_DEFINITIONS,
    TOOL_CONTROL_SECTION_TITLE_KEYS,
    type ToolControlSection,
} from '../constants/toolControls'
import { useT } from '../contexts/I18nContext'
import { drawerVariants } from '../utils/motion'
import './ProviderSettings.css'
import './Settings.css'

interface ToolSettingsProps {
    onBack: () => void
}

const TOOL_SECTION_ORDER: ToolControlSection[] = ['interaction', 'realtime', 'creation']

const ToolSettingsPanel: React.FC<ToolSettingsProps> = ({ onBack }) => {
    const { t } = useT()

    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [message, setMessage] = useState('')
    const [messageType, setMessageType] = useState<'success' | 'error'>('success')
    const [toolToggles, setToolToggles] = useState<Record<string, boolean>>(() => createDefaultToolToggles())

    const sections = useMemo(
        () =>
            TOOL_SECTION_ORDER.map((section) => ({
                key: section,
                titleKey: TOOL_CONTROL_SECTION_TITLE_KEYS[section],
                items: TOOL_CONTROL_DEFINITIONS.filter((tool) => tool.section === section),
            })),
        []
    )

    const showMessageToast = (msg: string, type: 'success' | 'error' = 'success') => {
        setMessage(msg)
        setMessageType(type)
        window.setTimeout(() => {
            setMessage('')
            setMessageType('success')
        }, 2000)
    }

    const loadData = async () => {
        setLoading(true)
        try {
            const data = await getProviders()
            if (!data) {
                throw new Error(t('common.loadFailed'))
            }
            setToolToggles(normalizeToolToggles(data.tool_toggles))
        } catch (error) {
            showMessageToast(getErrorMessage(error, t('common.loadFailed')), 'error')
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        void loadData()
    }, [])

    const enabledCount = countEnabledToolToggles(toolToggles)

    const handleToggle = (key: string, enabled: boolean) => {
        setToolToggles((current) => ({
            ...current,
            [key]: enabled,
        }))
    }

    const handleSave = async () => {
        if (saving) return
        setSaving(true)
        try {
            const success = await updateConfig({ tool_toggles: toolToggles })
            if (!success) {
                showMessageToast(t('common.saveFailed'), 'error')
                return
            }
            showMessageToast(t('settings.toolControlSaved'))
        } finally {
            setSaving(false)
        }
    }

    return (
        <motion.div
            className="provider-settings"
            initial="hidden"
            animate="visible"
            exit="hidden"
            variants={drawerVariants}
        >
            <div className="provider-settings-header">
                <button className="back-button" onClick={onBack}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('settings.tools')}</div>
                <div style={{ width: 44 }}></div>
            </div>

            {loading ? (
                <div className="provider-settings-loading">{t('common.loading')}</div>
            ) : (
                <div className="provider-settings-content">
                    <div className="settings-section">
                        <h3>{t('settings.tools')}</h3>
                        <p className="prompt-modal-hint">{t('settings.toolControlHint')}</p>
                        <div className="tool-control-summary">
                            {t('settings.toolControlSummary', {
                                enabled: enabledCount,
                                total: TOOL_CONTROL_DEFINITIONS.length,
                            })}
                        </div>
                    </div>

                    {sections.map((section) => (
                        <div className="settings-section" key={section.key}>
                            <h3>{t(section.titleKey)}</h3>
                            <div className="tool-control-list">
                                {section.items.map((tool) => {
                                    const enabled = toolToggles[tool.key] ?? true
                                    return (
                                        <div className="tool-control-card" key={tool.key}>
                                            <div className="tool-control-meta">
                                                <div className="tool-control-title-row">
                                                    <div className="tool-control-title">{t(tool.titleKey)}</div>
                                                    <span
                                                        className={`tool-control-status ${enabled ? 'enabled' : 'disabled'}`}
                                                    >
                                                        {enabled ? t('common.enabled') : t('common.disabled')}
                                                    </span>
                                                </div>
                                                <p className="tool-control-description">{t(tool.descriptionKey)}</p>
                                                {tool.hintKey && <p className="tool-control-hint">{t(tool.hintKey)}</p>}
                                            </div>
                                            <div className="modal-toggle-wrapper tool-control-toggle">
                                                <label className="toggle-switch">
                                                    <input
                                                        type="checkbox"
                                                        checked={enabled}
                                                        onChange={(event) =>
                                                            handleToggle(tool.key, event.target.checked)
                                                        }
                                                        disabled={saving}
                                                    />
                                                    <span className="toggle-slider"></span>
                                                </label>
                                                <span className="toggle-label">
                                                    {enabled ? t('common.enable') : t('common.disable')}
                                                </span>
                                            </div>
                                        </div>
                                    )
                                })}
                            </div>
                        </div>
                    ))}

                    <button className="settings-save-btn" onClick={handleSave} disabled={saving}>
                        {saving ? t('common.saving') : t('common.save')}
                    </button>

                    {message && <div className={`settings-message ${messageType}`}>{message}</div>}
                </div>
            )}
        </motion.div>
    )
}

export default ToolSettingsPanel
