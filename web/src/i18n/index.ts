import zh from './zh'
import en from './en'

export type Locale = 'zh' | 'en'

export type Translations = typeof zh

type NestedKeyOf<T, Prefix extends string = ''> = T extends string | readonly string[]
    ? Prefix
    : T extends Record<string, unknown>
      ? {
            [K in keyof T & string]: NestedKeyOf<T[K], Prefix extends '' ? K : `${Prefix}.${K}`>
        }[keyof T & string]
      : Prefix

export type TranslationKey = NestedKeyOf<Translations>

export const translations: Record<Locale, Translations> = { zh, en: en as unknown as Translations }

export const localeNames: Record<Locale, string> = {
    zh: '中文',
    en: 'English',
}

export const supportedLocales: Locale[] = ['zh', 'en']
export const LOCALE_STORAGE_KEY = 'cornerstone.locale'
export const DEFAULT_LOCALE: Locale = 'zh'

export function isLocale(value: unknown): value is Locale {
    return supportedLocales.includes(value as Locale)
}

export function getLocale(): Locale {
    if (typeof window === 'undefined') return DEFAULT_LOCALE

    const saved = window.localStorage.getItem(LOCALE_STORAGE_KEY)
    if (isLocale(saved)) return saved

    const lang = window.navigator.language || ''
    if (lang.startsWith('en')) return 'en'
    return DEFAULT_LOCALE
}

export function persistLocale(locale: Locale): void {
    if (typeof window === 'undefined') return
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale)
}

function getNestedValue(obj: Record<string, unknown>, path: string): unknown {
    return path.split('.').reduce<unknown>((acc, key) => {
        if (acc && typeof acc === 'object' && key in (acc as Record<string, unknown>)) {
            return (acc as Record<string, unknown>)[key]
        }
        return undefined
    }, obj)
}

export function interpolate(template: string, params?: Record<string, string | number>): string {
    if (!params) return template
    return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => {
        return key in params ? String(params[key]) : `{{${key}}}`
    })
}

export function translate(
    key: TranslationKey | string,
    params?: Record<string, string | number>,
    locale = getLocale()
): string {
    const value = getNestedValue(translations[locale] as unknown as Record<string, unknown>, key)
    if (typeof value === 'string') {
        return interpolate(value, params)
    }

    if (locale !== DEFAULT_LOCALE) {
        const fallback = getNestedValue(translations[DEFAULT_LOCALE] as unknown as Record<string, unknown>, key)
        if (typeof fallback === 'string') {
            return interpolate(fallback, params)
        }
    }

    return key
}
