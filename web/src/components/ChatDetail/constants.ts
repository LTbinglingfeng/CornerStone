import { translate } from '../../i18n'

export const ASSISTANT_BUBBLE_INTERVAL_MS = 1500
export const QUOTE_PREFIX_CANDIDATES = ['引用的信息:', '引用的信息：', 'Quoted message:']
export const RECALLED_MESSAGE_SUFFIX_CANDIDATES = ['(已撤回)', '(recalled)']

export const getQuotedMessagePrefix = (): string => translate('chat.quotedMessage')

export const getRecalledMessageSuffix = (): string => translate('chat.recalled')
