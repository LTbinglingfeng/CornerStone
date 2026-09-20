import type { Locale } from '../i18n'

const settingsManagementCopy = {
    zh: {
        eyebrow: '管理设置',
        navigationLabel: '设置分类',
        backLabel: '返回',
        sections: {
            models: {
                label: '模型与服务',
                description: '管理聊天与图像模型、联网搜索和语音服务。',
            },
            automation: {
                label: '工具与自动化',
                description: '配置可用工具、提醒任务和主动问候。',
            },
            memory: {
                label: '长期记忆',
                description: '控制记忆开关、提取模型、频率与提取提示词。',
            },
            general: {
                label: '通用',
                description: '调整回复行为、地区、通知、语言和其他全局选项。',
            },
        },
    },
    en: {
        eyebrow: 'Management settings',
        navigationLabel: 'Settings categories',
        backLabel: 'Back',
        sections: {
            models: {
                label: 'Models & services',
                description: 'Manage chat and image models, web search, and voice services.',
            },
            automation: {
                label: 'Tools & automation',
                description: 'Configure available tools, scheduled reminders, and proactive greetings.',
            },
            memory: {
                label: 'Memory',
                description: 'Control long-term memory, extraction models, frequency, and prompts.',
            },
            general: {
                label: 'General',
                description: 'Adjust reply behavior, locale, notifications, language, and global options.',
            },
        },
    },
} as const

export const getSettingsManagementCopy = (locale: Locale) => settingsManagementCopy[locale]
