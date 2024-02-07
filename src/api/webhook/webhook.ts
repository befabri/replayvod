import { env } from "../../app";
import { prisma } from "../../server";
import { logger as rootLogger } from "../../app";
import {
    StreamOfflineEvent,
    StreamOnlineEvent,
    SubscriptionType,
    TwitchEvent,
    TwitchNotificationBody,
    TwitchNotificationChallenge,
    TwitchNotificationEvent,
    TwitchNotificationRevocation,
} from "../../models/twitch";
import { transformWebhookEvent } from "./webhook.DTO";
import { channelFeature } from "../channel";
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

export async function handleChannelUpdate(notification: TwitchNotificationEvent) {
    logger.info(`Handling channel update for broadcaster ID: ${notification.event.broadcaster_user_id}`);
    // Add your logic here to handle channel updates
    // Example: Update channel info in your database
}

export async function handleStreamOnline(notification: TwitchNotificationEvent) {
    const event = notification.event as StreamOnlineEvent;
    logger.info(`Handling stream online for broadcaster ID: ${event.broadcaster_user_id}`);
    await createWebhookEvent(notification.subscription.type, event);
    const fetchStream = async (retryCount = 0) => {
        try {
            const streamFetched = await channelFeature.getChannelStream(
                notification.event.broadcaster_user_id,
                "system"
            );
            if (!streamFetched) {
                logger.error(
                    `OFFLINE? Stream fetched error in handleStreamOnline for ${event.broadcaster_user_id}`
                );
                if (retryCount < 5) {
                    setTimeout(() => fetchStream(retryCount + 1), 120000); // 120000 milliseconds = 2 minutes
                } else {
                    logger.error(`Max retries reached for ${event.broadcaster_user_id}`);
                }
            } else {
                logger.info(`Stream successfully fetched in handleStreamOnline for ${event.broadcaster_user_id}`);
            }
        } catch (error) {
            logger.error(`Stream fetched error in handleStreamOnline for ${event.broadcaster_user_id}: ${error}`);
            if (retryCount < 5) {
                setTimeout(() => fetchStream(retryCount + 1), 120000);
            } else {
                logger.error(`Max retries reached for ${event.broadcaster_user_id} after encountering an error`);
            }
        }
    };
    await fetchStream();
}

export async function handleStreamOffline(notification: TwitchNotificationEvent) {
    const event = notification.event as StreamOfflineEvent;
    logger.info(`Handling stream offline for broadcaster ID: ${event.broadcaster_user_id}`);
    await createWebhookEvent(notification.subscription.type, event);
    const lastStream = await channelFeature.getLastActiveStreamByBroadcaster(event.broadcaster_user_id);
    if (!lastStream) {
        logger.error(`Stream not found in handleStreamOffline for ${event.broadcaster_user_id}`);
        return;
    }
    await channelFeature.updateStreamEnded(lastStream.id);
}

export async function handleNotification(notification: TwitchNotificationBody) {
    logger.info(`Handling generic notification for subscription type: ${notification.subscription.type}`);
    // Add your generic handling logic here
    // Example: Log the notification or perform a generic update
}

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

export const createWebhookEvent = async (eventType: SubscriptionType, event: TwitchEvent) => {
    try {
        const webhookEvent = transformWebhookEvent(eventType, event.broadcaster_user_id);
        if (!webhookEvent) {
            return;
        }
        await prisma.webhookEvent.create({
            data: {
                broadcasterId: webhookEvent.broadcasterId,
                eventType: webhookEvent.eventType,
                startedAt: webhookEvent.startedAt,
                endAt: webhookEvent.endAt,
            },
        });
    } catch (error) {
        logger.error(
            "Error in handleWebhookEvent with eventType: %s and broadcasterId: %s - %s",
            eventType,
            event.broadcaster_user_id,
            error
        );
    }
};
