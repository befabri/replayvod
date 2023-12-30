export interface TwitchUserData {
    id: string;
    login: string;
    display_name: string;
    type: string;
    broadcaster_type: string;
    description: string;
    profile_image_url: string;
    offline_image_url: string;
    view_count?: number;
    email: string;
    created_at: Date;
}

export interface TwitchToken {
    access_token: string;
    expires_in: number;
    refresh_token?: string;
    token_type: string;
    expires_at: Date;
}

export interface TwitchTokenResponse {
    access_token: string;
    expires_in: number;
    refresh_token?: string;
    token_type: string;
    scope: string[];
}

export interface UserSession {
    twitchToken: TwitchToken;
    twitchUserID: string;
    twitchUserData: TwitchUserData;
}
