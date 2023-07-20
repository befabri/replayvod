import { Document, ObjectId } from "mongodb";

export interface Webhook extends Document {
    _id?: ObjectId;
    id: string;
    url?: string;
    status: string;
    type: string;
    version: string;
    condition: {
        broadcaster_user_id: string;
    };
    transport: {
        method: string;
        callback: string;
    };
    created_at: string;
    event: {
        user_id: string;
        user_login: string;
        user_name: string;
        broadcaster_user_id: string;
        broadcaster_user_login: string;
        broadcaster_user_name: string;
        followed_at: string;
    };
}

export interface EventSub {
    _id?: string;
    id: string;
    status: string;
    type: string;
    user_id: string;
    user_login?: string;
    created_at: Date;
    cost: number;
}
