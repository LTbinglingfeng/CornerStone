import { createContext, useContext, useState, useCallback, useMemo, ReactNode } from 'react'
import { getLocale, persistLocale, translate, type Locale, type TranslationKey } from '../i18n'

interface I18nContextType {
    locale: Locale
    setLocale: (locale: Locale) => void
    t: (key: TranslationKey, params?: Record<string, string | number>) => string
}

const I18nContext = createContext<I18nContextType | undefined>(undefined)

export const I18nProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
    const [locale, setLocaleState] = useState<Locale>(getLocale)

    const setLocale = useCallback((newLocale: Locale) => {
        setLocaleState(newLocale)
        persistLocale(newLocale)
    }, [])

    const t = useCallback(
        (key: TranslationKey, params?: Record<string, string | number>): string => translate(key, params, locale),
        [locale]
    )

    const value = useMemo(() => ({ locale, setLocale, t }), [locale, setLocale, t])

    return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export const useT = () => {
    const context = useContext(I18nContext)
    if (!context) {
        throw new Error('useT must be used within an I18nProvider')
    }
    return context
}

export const useTranslation = useT
