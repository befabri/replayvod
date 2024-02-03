import { env } from "../../app";
import { prisma } from "../../server";
import { logger as rootLogger } from "../../app";
import {
    SubscriptionType,
    TwitchEvent,
    TwitchNotificationBody,
    TwitchNotificationChallenge,
    TwitchNotificationEvent,
    TwitchNotificationRevocation,
} from "../../models/twitch";
import { transformWebhookEvent } from "./webhook.DTO";
import { eventSubFeature } from "../event-sub";
const logger = rootLogger.child({ domain: "webhook", service: "webhookFeature" });

export const getWebhook = async (id: string) => {
    // return prisma.event.findUnique({
    //     where: { id: id },
    // });
    return id;
};

export const getAllWebhooks = async () => {
    return prisma.webhookEvent.findMany();
};

export const getSecret = () => {
    return env.secret;
};

export const getCallbackUrlWebhook = (): string => {
    return env.callbackUrlWebhook;
};

export const isTwitchNotificationEvent = (
    notification: TwitchNotificationBody
): notification is TwitchNotificationEvent => {
    return "event" in notification;
};

export const isTwitchNotificationChallenge = (
    notification: TwitchNotificationBody
): notification is TwitchNotificationChallenge => {
    return "challenge" in notification;
};

export const isTwitchNotificationRevocation = (
    notification: TwitchNotificationBody
): notification is TwitchNotificationRevocation => {
    return !("event" in notification) && !("challenge" in notification);
};

export const handleRevocation = (notification: any) => {
    // Implementation for handling revocation
    logger.info("Received a revocation:");
    logger.info(JSON.stringify(notification, null, 2));
    logger.info(`${notification.subscription.type} notifications revoked!`);
    logger.info(`Reason: ${notification.subscription.status}`);
    logger.info(`Condition: ${JSON.stringify(notification.subscription.condition, null, 4)}`);
    // TODO: Add any additional logic needed to handle revocation, such as
    // updating your application's internal state or notifying other services.
};

export const handleDownload = (event: any) => {
    logger.info(event, event.broadcaster_user_id);
    // const broadcaster_id = event.broadcaster_user_id;
    // downloadSchedule(broadcaster_id).catch((error) => {
    //     logger.error("Error in downloadSchedule:", error);
    // });
};

export const handleWebhookEvent = async (eventType: SubscriptionType, event: TwitchEvent) => {
    try {
        const webhookEvent = transformWebhookEvent(eventType, event.broadcaster_user_id);
        logger.info(`Transformed webhook: ${JSON.stringify(webhookEvent)}`);
        if (!webhookEvent) {
            return;
        }
        await eventSubFeature.addWebhookEvent(webhookEvent);
    } catch (error) {
        logger.error(
            "Error in handleWebhookEvent with eventType: %s and broadcasterId: %s - %s",
            eventType,
            event.broadcaster_user_id,
            error
        );
    }
};
