import type { Locale } from './index'

export const accountCopy = {
    en: {
        profile: {
            title: 'Profile',
            description: 'Your name, avatar and a short bio. Personas see this when they talk to you.',
            details: 'Details',
            changeAvatar: 'Change avatar',
            avatarHint: 'JPG, PNG or WebP. Shown in the sidebar and to your personas.',
            nickname: 'Nickname',
            bio: 'About you',
            bioHint: 'A few lines of context your personas can use, such as what you do or how you like replies.',
            save: 'Save',
            saving: 'Saving…',
            loadFailed: 'Could not load your profile.',
        },
        login: {
            title: 'Sign in',
            subtitle: 'Welcome back. Enter your password to continue.',
            submit: 'Sign in',
        },
        setup: {
            title: 'Create your account',
            subtitle: 'First run on this server. Choose a username and password for the console.',
            submit: 'Create account',
        },
    },
    zh: {
        profile: {
            title: '个人资料',
            description: '你的昵称、头像和简介。人设在与你对话时会参考这些信息。',
            details: '资料',
            changeAvatar: '更换头像',
            avatarHint: '支持 JPG、PNG 或 WebP，会显示在侧栏并提供给人设。',
            nickname: '昵称',
            bio: '个人简介',
            bioHint: '几句关于你的背景，例如职业或偏好的回复方式，人设可以参考。',
            save: '保存',
            saving: '保存中…',
            loadFailed: '个人资料加载失败。',
        },
        login: {
            title: '登录',
            subtitle: '欢迎回来，输入密码以继续。',
            submit: '登录',
        },
        setup: {
            title: '创建账户',
            subtitle: '首次启动。请为管理控制台设置用户名和密码。',
            submit: '创建账户',
        },
    },
} as const

export const getAccountCopy = (locale: Locale) => accountCopy[locale]
