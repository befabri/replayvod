import { STREAM_OFFLINE, STREAM_ONLINE } from "../../constants/twitchConstants";

export const transformWebhookEvent = (eventType: string, broadcasterId: string) => {
    if (eventType === STREAM_ONLINE) {
        return {
            externalEventId: "",
            broadcasterId: broadcasterId,
            eventType: eventType,
            startedAt: new Date(),
            endAt: null,
        };
    } else if (eventType === STREAM_OFFLINE) {
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
