import { SubscriptionType } from "../../models/twitchModel";

export const transformWebhookEvent = (eventType: string, broadcasterId: string) => {
    if (eventType === SubscriptionType.STREAM_ONLINE) {
        return {
            externalEventId: "",
            broadcasterId: broadcasterId,
            eventType: eventType,
            startedAt: new Date(),
            endAt: null,
        };
    } else if (eventType === SubscriptionType.STREAM_OFFLINE) {
        return {
            externalEventId: "",
            broadcasterId: broadcasterId,
            eventType: eventType,
            startedAt: null,
            endAt: new Date(),
        };
    } else {
        return null;
    }
};
