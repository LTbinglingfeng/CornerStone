export type MomentStatus = 'pending' | 'generating' | 'published' | 'failed'

export interface Moment {
    id: string
    prompt_id: string
    prompt_name: string
    content: string
    image_prompt: string
    image_path: string
    status: MomentStatus
    error_msg?: string
    created_at: string
    updated_at: string
    likes: Like[]
    comments: Comment[]
}

export interface Like {
    id: string
    user_type: 'user' | 'prompt'
    user_id: string
    user_name: string
    created_at: string
}

export interface Comment {
    id: string
    user_type: 'user' | 'prompt'
    user_id: string
    user_name: string
    content: string
    reply_to?: string
    created_at: string
}

export interface MomentsConfig {
    background_image?: string
}

export interface CreateMomentRequest {
    prompt_id: string
    prompt_name: string
    content: string
    image_prompt: string
}

export interface AddLikeRequest {
    user_type: 'user' | 'prompt'
    user_id: string
    user_name: string
}

export interface AddCommentRequest {
    user_type: 'user' | 'prompt'
    user_id: string
    user_name: string
    content: string
    reply_to?: string
}
