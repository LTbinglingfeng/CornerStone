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
import { useToast } from '../contexts/ToastContext'
import { Group, Row, Toggle } from './ui'
import { drawerVariants } from '../utils/motion'
import './ProviderSettings.css'
import './Settings.css'

interface ToolSettingsProps {
    onBack: () => void
}

const TOOL_SECTION_ORDER: ToolControlSection[] = ['interaction', 'realtime']

const ToolSettingsPanel: React.FC<ToolSettingsProps> = ({ onBack }) => {
    const { t } = useT()
    const { showToast } = useToast()

    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
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

    const showMessageToast = (msg: string, type: 'success' | 'error' = 'success') => showToast(msg, type)

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
                <button className="back-button" onClick={onBack} aria-label={t('common.back')}>
                    <svg viewBox="0 0 24 24">
                        <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
                    </svg>
                </button>
                <div className="provider-settings-title">{t('settings.tools')}</div>
                {!loading && (
                    <button
                        type="button"
                        className="ui-btn ui-btn--primary ui-btn--sm"
                        onClick={() => void handleSave()}
                        disabled={saving}
                    >
                        {saving ? t('common.saving') : t('common.save')}
                    </button>
                )}
            </div>

            {loading ? (
                <div className="provider-settings-loading" role="status">
                    <span className="ui-spinner" aria-hidden="true" />
                    {t('common.loading')}
                </div>
            ) : (
                <div className="provider-settings-content">
                    <p className="tool-settings-summary">
                        <span>
                            {t('settings.toolControlSummary', {
                                enabled: enabledCount,
                                total: TOOL_CONTROL_DEFINITIONS.length,
                            })}
                        </span>
                        {t('settingsManagement.tools.description')}
                    </p>

                    {sections.map((section) => (
                        <Group title={t(section.titleKey)} key={section.key}>
                            {section.items.map((tool) => {
                                const enabled = toolToggles[tool.key] ?? true
                                return (
                                    <Row
                                        key={tool.key}
                                        label={t(tool.titleKey)}
                                        detail={
                                            tool.hintKey ? (
                                                <>
                                                    {t(tool.descriptionKey)}
                                                    <span className="tool-settings-hint">{t(tool.hintKey)}</span>
                                                </>
                                            ) : (
                                                t(tool.descriptionKey)
                                            )
                                        }
                                        trailing={
                                            <Toggle
                                                label={t(tool.titleKey)}
                                                checked={enabled}
                                                disabled={saving}
                                                onChange={(checked) => handleToggle(tool.key, checked)}
                                            />
                                        }
                                    />
                                )
                            })}
                        </Group>
                    ))}
                </div>
            )}
        </motion.div>
    )
}

export default ToolSettingsPanel
