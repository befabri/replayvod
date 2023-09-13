export enum SubscriptionType {
    CHANNEL_UPDATE = "CHANNEL_UPDATE",
    STREAM_ONLINE = "STREAM_ONLINE",
    STREAM_OFFLINE = "STREAM_OFFLINE",
}

export interface Subscription {
    type: SubscriptionType;
    // Add other properties as necessary
}

export interface NotificationBody {
    subscription: Subscription;
    // Add other properties as necessary
}
