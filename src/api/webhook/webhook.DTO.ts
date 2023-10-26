export const transformWebhookEvent = (eventType: string, broadcasterId: string) => {
    if (eventType === "stream.online") {
        return {
            externalEventId: "",
            broadcasterId: broadcasterId,
            eventType: eventType,
            startedAt: null,
            endAt: new Date(),
        };
    } else if (eventType === "stream.offline") {
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
