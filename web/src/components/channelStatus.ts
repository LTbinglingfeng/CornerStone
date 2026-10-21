import type { Tone } from './ui'

/** Maps ClawBot / NapCat / QQ Official status strings onto badge tones. */
export const channelStatusTone = (status: string | undefined): Tone => {
    if (status === 'running' || status === 'connected') return 'success'
    if (status === 'error') return 'danger'
    if (status === 'missing_token' || status === 'token_expired' || status === 'missing_credentials') return 'warning'
    if (status === 'connecting' || status === 'reconnecting') return 'info'
    return 'neutral'
}
