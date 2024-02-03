export interface StreamResponse {
    id: string;
    user_id: string;
    user_login: string;
    user_name: string;
    game_id: string;
    game_name: string;
    type: string;
    title: string;
    tags: string[];
    viewer_count: number;
    started_at: string;
    language: string;
    thumbnail_url: string;
    tag_ids: string[];
    is_mature?: boolean;
}

export interface GameResponse {
    id: string;
    name: string;
    box_art_url: string;
    igdb_id: string;
}

export interface UserResponse {
    id: string;
    login: string;
    display_name: string;
    type: string;
    broadcaster_type: string;
    description: string;
    profile_image_url: string;
    offline_image_url: string;
    view_count: number;
    email: string;
    created_at: string;
}

export interface FollowedChannelResponse {
    broadcaster_id: string;
    broadcaster_login: string;
    broadcaster_name: string;
    followed_at: string;
}

export interface EventSubData {
    id: string;
    status: string;
    type: string;
    version: string;
    condition: {
        broadcaster_user_id?: string;
        user_id?: string;
    };
    created_at: string;
    transport: {
        method: string;
        callback: string;
    };
    cost: number;
}

export interface EventSubResponse {
    data: EventSubData[];
    total: number;
    total_cost: number;
    max_total_cost: number;
    pagination: any;
}

// Custom not directly from twitch. Link to EventSubResponse
export interface EventSubMeta {
    total: number;
    total_cost: number;
    max_total_cost: number;
}
