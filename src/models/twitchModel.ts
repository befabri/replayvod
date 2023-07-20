import { Document, ObjectId } from "mongodb";

export interface Stream {
    _id?: ObjectId;
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
    is_mature: boolean;
}

export interface Game {
    _id?: ObjectId;
    id: string;
    name: string;
    box_art_url: string;
    igdb_id: string;
}

export interface User {
    _id?: ObjectId;
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

export interface FollowedChannel {
    _id?: ObjectId;
    broadcaster_id: string;
    broadcaster_login: string;
    broadcaster_name: string;
    followed_at: string;
}

export interface FollowedStream {
    _id?: ObjectId;
    id: string;
    user_id: string;
    user_login: string;
    user_name: string;
    game_id: string;
    game_name: string;
    type: string;
    title: string;
    viewer_count: number;
    started_at: string;
    language: string;
    thumbnail_url: string;
    tag_ids: string[];
    tags: string[];
}

export interface EventSubResponse {
    total: number;
    data: {
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
    }[];
    total_cost: number;
    max_total_cost: number;
    pagination: any;
}
