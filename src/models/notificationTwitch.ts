export enum SubscriptionType {
    CHANNEL_UPDATE = "channel.update",
    STREAM_ONLINE = "stream.online",
    STREAM_OFFLINE = "stream.offline",
}

export interface Subscription {
    type: SubscriptionType;
    // Add other properties as necessary
}

export interface NotificationBody {
    subscription: Subscription;
    // Add other properties as necessary
}
