import type { Locale } from './index'

export const settingsManagementCopy = {
    zh: {
        navigationLabel: '设置分类',
        backLabel: '返回',
        sections: {
            models: {
                label: '模型',
                description: '聊天与生图使用的模型、联网搜索和语音。',
            },
            automation: {
                label: '工具与自动化',
                description: '决定 AI 可以调用哪些工具，以及何时主动联系你。',
            },
            memory: {
                label: '记忆',
                description: '让 AI 记住长期信息，并控制提取的频率与方式。',
            },
            general: {
                label: '通用',
                description: '回复方式、地区、外观与账户。',
            },
        },
        groups: {
            services: '模型服务',
            tools: '工具',
            automation: '自动任务',
            memory: '长期记忆',
            extraction: '记忆提取',
            reply: '回复',
            region: '地区',
            preferences: '偏好',
            account: '账户',
            about: '关于',
        },
        rows: {
            chatProvider: '聊天供应商',
            toolsValue: '已开启 {{enabled}}/{{total}}',
            appearance: '外观',
            light: '浅色',
            dark: '深色',
            profile: '个人资料',
            version: '当前版本',
            notificationsDenied: '浏览器已拒绝通知权限',
            notificationsUnsupported: '当前浏览器不支持通知',
            memoryModelFollow: '跟随聊天模型',
        },
        provider: {
            add: '添加',
            basics: '基本信息',
            connection: '连接',
            parameters: '生成参数',
            capabilities: '能力',
            empty: '还没有供应商',
            emptyHint: '添加一个供应商后即可开始对话。',
            vision: '识图',
        },
        imageProvider: {
            autoDetail: '自动使用第一个生图供应商',
            listTitle: '生图供应商',
        },
        memoryProvider: {
            followDetail: '使用当前聊天供应商',
            independentDetail: '单独配置一个更快、更便宜的模型',
        },
        webSearch: {
            service: '搜索服务',
            results: '结果',
            excludeDomains: '排除域名',
        },
        tools: {
            description: '关闭后，模型将看不到对应工具，也不会收到相关调用指引。',
        },
        reminders: {
            emptyHint: 'AI 在对话中创建提醒后会出现在这里。',
            content: '内容',
            details: '详情',
        },
        idleGreeting: {
            enable: '启用主动问候',
            minutes: '分钟',
            addWindow: '添加时段',
            removeWindow: '删除时段',
            to: '至',
        },
    },
    en: {
        navigationLabel: 'Settings categories',
        backLabel: 'Back',
        sections: {
            models: {
                label: 'Models',
                description: 'Models for chat and images, web search, and voice.',
            },
            automation: {
                label: 'Tools & automation',
                description: 'Choose which tools the AI can use and when it reaches out on its own.',
            },
            memory: {
                label: 'Memory',
                description: 'Let the AI remember long-term facts and control how often it extracts them.',
            },
            general: {
                label: 'General',
                description: 'Replies, region, appearance, and account.',
            },
        },
        groups: {
            services: 'Model services',
            tools: 'Tools',
            automation: 'Automations',
            memory: 'Long-term memory',
            extraction: 'Extraction',
            reply: 'Replies',
            region: 'Region',
            preferences: 'Preferences',
            account: 'Account',
            about: 'About',
        },
        rows: {
            chatProvider: 'Chat provider',
            toolsValue: '{{enabled}}/{{total}} on',
            appearance: 'Appearance',
            light: 'Light',
            dark: 'Dark',
            profile: 'Profile',
            version: 'Version',
            notificationsDenied: 'Notification permission was denied by the browser',
            notificationsUnsupported: 'This browser does not support notifications',
            memoryModelFollow: 'Follow chat model',
        },
        provider: {
            add: 'Add',
            basics: 'Basics',
            connection: 'Connection',
            parameters: 'Generation',
            capabilities: 'Capabilities',
            empty: 'No providers yet',
            emptyHint: 'Add a provider to start chatting.',
            vision: 'Vision',
        },
        imageProvider: {
            autoDetail: 'Use the first image provider automatically',
            listTitle: 'Image providers',
        },
        memoryProvider: {
            followDetail: 'Use the current chat provider',
            independentDetail: 'Configure a separate, faster and cheaper model',
        },
        webSearch: {
            service: 'Search service',
            results: 'Results',
            excludeDomains: 'Excluded domains',
        },
        tools: {
            description: 'When a tool is off, the model no longer sees it or receives guidance for calling it.',
        },
        reminders: {
            emptyHint: 'Reminders the AI creates during chats appear here.',
            content: 'Content',
            details: 'Details',
        },
        idleGreeting: {
            enable: 'Enable idle greetings',
            minutes: 'min',
            addWindow: 'Add time window',
            removeWindow: 'Remove time window',
            to: 'to',
        },
    },
} as const

export const getSettingsManagementCopy = (locale: Locale) => settingsManagementCopy[locale]
