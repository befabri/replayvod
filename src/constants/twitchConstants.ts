export const TWITCH_MESSAGE_ID = "Twitch-Eventsub-Message-Id".toLowerCase();
export const TWITCH_MESSAGE_TIMESTAMP = "Twitch-Eventsub-Message-Timestamp".toLowerCase();
export const TWITCH_MESSAGE_SIGNATURE = "Twitch-Eventsub-Message-Signature".toLowerCase();
export const MESSAGE_TYPE = "Twitch-Eventsub-Message-Type".toLowerCase();
export const MESSAGE_TYPE_NOTIFICATION = "notification";
export const MESSAGE_TYPE_VERIFICATION = "webhook_callback_verification";
export const MESSAGE_TYPE_REVOCATION = "revocation";
export const HMAC_PREFIX = "sha256=";
export const CHANNEL_UPDATE = "channel.update";
export const STREAM_ONLINE = "stream.online";
export const STREAM_OFFLINE = "stream.offline";
export interface TwitchHeaders {
    "twitch-eventsub-message-id": string;
    "twitch-eventsub-message-retry"?: string;
    "twitch-eventsub-message-type": "notification" | "webhook_callback_verification" | "revocation";
    "twitch-eventsub-message-signature": string;
    "twitch-eventsub-message-timestamp": string;
    "twitch-eventsub-subscription-type": string;
    "twitch-eventsub-subscription-version": string;
    [key: string]: string | undefined;
}
