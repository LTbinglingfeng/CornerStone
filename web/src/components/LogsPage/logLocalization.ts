import { logVocabularyEn, logVocabularyZh } from '../../i18n/logVocabulary.ts'
import type { Locale } from '../../i18n'
import type { LogField } from '../../types/logs'

const vocabulary = (locale: Locale) => (locale === 'zh' ? logVocabularyZh : logVocabularyEn)
const lookup = (values: Record<string, string> | undefined, key: string) =>
    values && Object.prototype.hasOwnProperty.call(values, key) ? values[key] : key

/** Exact, field-scoped lookups only: never translate free text or identifiers. */
export const localizeLogEvent = (event: string, locale: Locale) => lookup(vocabulary(locale).events, event)
export const localizeLogField = (key: string, locale: Locale) => lookup(vocabulary(locale).fields, key)
export const localizeLogValue = (field: LogField, locale: Locale) => {
    const values = vocabulary(locale).values
    return lookup(Object.prototype.hasOwnProperty.call(values, field.key) ? values[field.key] : undefined, field.value)
}
