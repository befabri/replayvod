export enum SubscriptionType {
    CHANNEL_UPDATE = "channel.update",
    STREAM_ONLINE = "stream.online",
    STREAM_OFFLINE = "stream.offline",
    CHANNEL_FOLLOW = "channel.follow",
}

export enum TwitchStatus {
    ENABLED = "enabled",
    WEBHOOK_CALLBACK_VERIFICATION_PENDING = "webhook_callback_verification_pending",
    WEBHOOK_CALLBACK_VERIFICATION_FAILED = "webhook_callback_verification_failed",
    NOTIFICATION_FAILURES_EXCEEDED = "notification_failures_exceeded",
    AUTHORIZATION_REVOKED = "authorization_revoked",
    MODERATOR_REMOVED = "moderator_removed",
    USER_REMOVED = "user_removed",
    VERSION_REMOVED = "version_removed",
    BETA_MAINTENANCE = "beta_maintenance",
    WEBSOCKET_DISCONNECTED = "websocket_disconnected",
    WEBSOCKET_FAILED_PING_PONG = "websocket_failed_ping_pong",
    WEBSOCKET_RECEIVED_INBOUND_TRAFFIC = "websocket_received_inbound_traffic",
    WEBSOCKET_CONNECTION_UNUSED = "websocket_connection_unused",
    WEBSOCKET_INTERNAL_ERROR = "websocket_internal_error",
    WEBSOCKET_NETWORK_TIMEOUT = "websocket_network_timeout",
    WEBSOCKET_NETWORK_ERROR = "websocket_network_error",
}

export interface TwitchSubscription {
    id: string;
    type: SubscriptionType;
    version: string;
    status: TwitchStatus;
    cost: number;
    condition: {
        broadcaster_user_id: string;
    };
    transport: {
        method: "webhook";
        callback: string;
    };
    created_at: string;
}

export interface TwitchEvent {
    broadcaster_user_id: string;
    broadcaster_user_login: string;
    broadcaster_user_name: string;
}

export interface StreamOfflineEvent extends TwitchEvent {}

export interface StreamOnlineEvent extends TwitchEvent {
    id: string;
    type: "live" | "playlist" | "watch_party" | "premiere" | "rerun";
    started_at: string;
}

export interface TwitchNotificationBody {
    message_type: MessageType;
    subscription: TwitchSubscription;
}

export interface TwitchNotificationEvent extends TwitchNotificationBody {
    event: TwitchEvent;
}

export interface TwitchNotificationChallenge extends TwitchNotificationBody {
    challenge: string;
}

export interface TwitchNotificationRevocation extends TwitchNotificationBody {}

export enum MessageType {
    MESSAGE_TYPE_NOTIFICATION = "notification",
    MESSAGE_TYPE_VERIFICATION = "webhook_callback_verification",
    MESSAGE_TYPE_REVOCATION = "revocation",
}

export interface TwitchHeaders {
    "twitch-eventsub-message-id": string;
    "twitch-eventsub-message-retry"?: string;
    "twitch-eventsub-message-type":
        | MessageType.MESSAGE_TYPE_NOTIFICATION
        | MessageType.MESSAGE_TYPE_VERIFICATION
        | MessageType.MESSAGE_TYPE_REVOCATION;
    "twitch-eventsub-message-signature": string;
    "twitch-eventsub-message-timestamp": string;
    "twitch-eventsub-subscription-type": string;
    "twitch-eventsub-subscription-version": string;
    [key: string]: string | undefined;
}

export const TWITCH_MESSAGE_ID = "twitch-eventsub-message-id";
export const TWITCH_MESSAGE_RETRY = "twitch-eventsub-message-retry";
export const MESSAGE_TYPE = "twitch-eventsub-message-type";
export const TWITCH_MESSAGE_SIGNATURE = "twitch-eventsub-message-signature";
export const TWITCH_MESSAGE_TIMESTAMP = "twitch-eventsub-message-timestamp";
export const TWITCH_SUBSCRIPTION_TYPE = "twitch-eventsub-subscription-type";
export const TWITCH_SUBSCRIPTION_VERSION = "twitch-eventsub-subscription-version";
export const HMAC_PREFIX = "sha256=";
export const TWITCH_ENDPOINT = "/api/auth/twitch";
export const FETCH_MAX_RETRIES = 3;
export const FETCH_RETRY_DELAY = 1000;
