import { useEffect, useState } from 'react'

export type ManagementResource<T> = { phase: 'loading' | 'error'; data?: never } | { phase: 'ready'; data: T }

// Existing service methods do not accept AbortSignal. Bound the visible wait
// and ignore late responses so refreshes/unmounts cannot overwrite newer data.
export function useManagementResource<T>(load: () => Promise<T>, revision: number): ManagementResource<T> {
    const [result, setResult] = useState<ManagementResource<T>>({ phase: 'loading' })
    useEffect(() => {
        let active = true
        setResult({ phase: 'loading' })
        const timeout = window.setTimeout(() => {
            if (active) setResult({ phase: 'error' })
            active = false
        }, 20000)
        void load().then(
            (data) => {
                if (active) setResult({ phase: 'ready', data })
                window.clearTimeout(timeout)
            },
            () => {
                if (active) setResult({ phase: 'error' })
                window.clearTimeout(timeout)
            }
        )
        return () => {
            active = false
            window.clearTimeout(timeout)
        }
    }, [load, revision])
    return result
}
