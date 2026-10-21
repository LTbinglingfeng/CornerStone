import type { Tone } from './ui'

/** Maps ClawBot / NapCat status strings onto badge tones. */
export const channelStatusTone = (status: string | undefined): Tone => {
    if (status === 'running' || status === 'connected') return 'success'
    if (status === 'error') return 'danger'
    if (status === 'missing_token' || status === 'token_expired') return 'warning'
    if (status === 'connecting') return 'info'
    return 'neutral'
}
