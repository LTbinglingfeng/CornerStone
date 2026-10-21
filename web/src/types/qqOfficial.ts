export interface QQOfficialSettings {
    enabled: boolean
    app_id: string
    has_secret: boolean
    prompt_id: string
    prompt_name?: string
    source_filter_mode: 'all' | 'allowlist'
    allowed_user_openids: string[]
    status: 'disabled' | 'missing_credentials' | 'connecting' | 'connected' | 'reconnecting' | 'error' | 'disconnected'
    last_error: string
    bot_id: string
    bot_name: string
}
export type QQOfficialUpdate = Pick<
    QQOfficialSettings,
    'enabled' | 'app_id' | 'prompt_id' | 'source_filter_mode' | 'allowed_user_openids'
> & { app_secret?: string; clear_secret?: boolean }
export interface QQOfficialLogin {
    id: string
    qr_url: string
    expires_at: string
    interval_seconds: number
    status: 'pending'
}
export interface QQOfficialLoginStatus {
    id: string
    status: 'pending' | 'created' | 'expired' | 'cancelled' | 'error'
    app_id?: string
    error?: string
}
