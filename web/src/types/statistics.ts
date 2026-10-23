/** RFC3339 timestamps; history is [from, to). Live rates ignore these bounds. */
export interface StatisticsQuery {
    from?: string
    to?: string
    provider_id?: string
    model?: string
}

export interface StatisticsTotals {
    requests: number
    successful_requests: number
    failed_requests: number
    usage_reported_requests: number
    unknown_usage_requests: number
    input_tokens: number
    output_tokens: number
    total_tokens: number
    cache_read_tokens: number
    cache_creation_tokens: number
    reasoning_tokens: number
    duration_ms: number
    /** 0..1, based on reported input tokens; null when the denominator is zero. */
    cache_hit_rate: number | null
    usage_coverage: number | null
}

export interface StatisticsSummary {
    filters: Required<StatisticsQuery>
    as_of: string
    totals: StatisticsTotals
    groups: {
        provider_id: string
        model: string
        totals: StatisticsTotals
    }[]
    realtime: {
        window_seconds: number
        rpm: number
        tpm: number
        usage_reported_requests: number
        unknown_usage_requests: number
        usage_coverage: number | null
    }
    /** Persistence health for this manager lifetime, not usage completeness. */
    data_complete: boolean
    write_failures: number
    write_failure_scope: 'manager_lifetime'
}
