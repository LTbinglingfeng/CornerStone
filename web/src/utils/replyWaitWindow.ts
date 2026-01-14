export type ReplyWaitWindowMode = 'fixed' | 'sliding'

export type ReplyWaitWindowConfig = {
  mode: ReplyWaitWindowMode
  seconds: number
}

const STORAGE_KEY = 'cornerstone.reply_wait_window'

const DEFAULT_CONFIG: ReplyWaitWindowConfig = {
  mode: 'sliding',
  seconds: 2,
}

const isValidMode = (value: unknown): value is ReplyWaitWindowMode => value === 'fixed' || value === 'sliding'

const normalizeSeconds = (value: unknown): number => {
  const num = typeof value === 'number' ? value : Number.parseFloat(String(value))
  if (!Number.isFinite(num)) return DEFAULT_CONFIG.seconds
  if (num < 0) return 0
  if (num > 120) return 120
  return Math.round(num)
}

export function getReplyWaitWindowConfig(): ReplyWaitWindowConfig {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULT_CONFIG
    const parsed = JSON.parse(raw) as Partial<ReplyWaitWindowConfig> | null
    if (!parsed || typeof parsed !== 'object') return DEFAULT_CONFIG

    const mode = isValidMode(parsed.mode) ? parsed.mode : DEFAULT_CONFIG.mode
    const seconds = normalizeSeconds(parsed.seconds)
    return { mode, seconds }
  } catch {
    return DEFAULT_CONFIG
  }
}

export function setReplyWaitWindowConfig(config: ReplyWaitWindowConfig): void {
  const normalized: ReplyWaitWindowConfig = {
    mode: isValidMode(config.mode) ? config.mode : DEFAULT_CONFIG.mode,
    seconds: normalizeSeconds(config.seconds),
  }
  localStorage.setItem(STORAGE_KEY, JSON.stringify(normalized))
}

export function formatReplyWaitWindowConfig(config: ReplyWaitWindowConfig): string {
  const normalized = {
    mode: isValidMode(config.mode) ? config.mode : DEFAULT_CONFIG.mode,
    seconds: normalizeSeconds(config.seconds),
  }
  if (normalized.seconds <= 0) return '立即发送'
  if (normalized.mode === 'fixed') return `固定 ${normalized.seconds}s`
  return `滑动 ${normalized.seconds}s`
}

