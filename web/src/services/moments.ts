import type { ApiResponse } from '../types/chat'
import type {
    AddCommentRequest,
    AddLikeRequest,
    Comment,
    CreateMomentRequest,
    Moment,
    MomentsConfig,
} from '../types/moments'
import { apiFetchJson, appendQueryParam } from './api'

const API_BASE = '/api'

export async function getMoments(limit = 20, offset = 0): Promise<Moment[]> {
    try {
        const data = await apiFetchJson<ApiResponse<Moment[]>>(`${API_BASE}/moments?limit=${limit}&offset=${offset}`)
        return data.success && data.data ? data.data : []
    } catch {
        return []
    }
}

export async function getMoment(id: string): Promise<Moment | null> {
    try {
        const data = await apiFetchJson<ApiResponse<Moment>>(`${API_BASE}/moments/${encodeURIComponent(id)}`)
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function createMoment(req: CreateMomentRequest): Promise<Moment | null> {
    try {
        const data = await apiFetchJson<ApiResponse<Moment>>(`${API_BASE}/moments`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(req),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function deleteMoment(id: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<string>>(`${API_BASE}/moments/${encodeURIComponent(id)}`, {
            method: 'DELETE',
        })
        return data.success
    } catch {
        return false
    }
}

export async function likeMoment(momentId: string, req: AddLikeRequest): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<boolean>>(`${API_BASE}/moments/${encodeURIComponent(momentId)}/like`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(req),
        })
        return data.success
    } catch {
        return false
    }
}

export async function unlikeMoment(momentId: string, userType: string, userId: string): Promise<boolean> {
    try {
        let url = `${API_BASE}/moments/${encodeURIComponent(momentId)}/like`
        url = appendQueryParam(url, 'user_type', userType)
        url = appendQueryParam(url, 'user_id', userId)
        const data = await apiFetchJson<ApiResponse<boolean>>(url, { method: 'DELETE' })
        return data.success
    } catch {
        return false
    }
}

export async function addComment(momentId: string, req: AddCommentRequest): Promise<Comment | null> {
    try {
        const data = await apiFetchJson<ApiResponse<Comment>>(`${API_BASE}/moments/${encodeURIComponent(momentId)}/comments`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(req),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function deleteComment(momentId: string, commentId: string): Promise<boolean> {
    try {
        const data = await apiFetchJson<ApiResponse<boolean>>(
            `${API_BASE}/moments/${encodeURIComponent(momentId)}/comments/${encodeURIComponent(commentId)}`,
            {
                method: 'DELETE',
            }
        )
        return data.success
    } catch {
        return false
    }
}

export async function getMomentsConfig(): Promise<MomentsConfig> {
    try {
        const data = await apiFetchJson<ApiResponse<MomentsConfig>>(`${API_BASE}/moments/config`)
        return data.success && data.data ? data.data : {}
    } catch {
        return {}
    }
}

export async function updateMomentsConfig(config: MomentsConfig): Promise<MomentsConfig | null> {
    try {
        const data = await apiFetchJson<ApiResponse<MomentsConfig>>(`${API_BASE}/moments/config`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(config),
        })
        return data.success && data.data ? data.data : null
    } catch {
        return null
    }
}

export async function uploadBackground(file: File): Promise<string | null> {
    try {
        const formData = new FormData()
        formData.append('background', file)

        const data = await apiFetchJson<ApiResponse<{ path: string }>>(`${API_BASE}/moments/config/background`, {
            method: 'POST',
            body: formData,
        })
        return data.success && data.data?.path ? data.data.path : null
    } catch {
        return null
    }
}

