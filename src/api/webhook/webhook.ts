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
import { scheduleFeature } from "../schedule";
import { downloadFeature } from "../download";
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
}

export async function handleStreamOnline(notification: TwitchNotificationEvent) {
    const event = notification.event as StreamOnlineEvent;
    logger.info({
        broadcasterID: event.broadcaster_user_id,
        message: `Stream online handling initiated.`,
        action: "streamOnlineHandlingStart",
    });
    await createWebhookEvent(notification.subscription.type, event);
    const fetchStream = async (retryCount = 0) => {
        const logContext = {
            broadcasterID: event.broadcaster_user_id,
            retryAttempt: retryCount,
            action: "fetchStreamAttempt",
        };
        try {
            const streamFetched = await channelFeature.getChannelStream(
                notification.event.broadcaster_user_id,
                "system"
            );
            if (!streamFetched) {
                logger.warn({
                    ...logContext,
                    message: "Stream OFFLINE or not started. Retrying...",
                    status: "offlineOrNotStarted",
                });
                if (retryCount < 5) {
                    setTimeout(() => fetchStream(retryCount + 1), 120000); // 120000 milliseconds = 2 minutes
                } else {
                    logger.error({
                        ...logContext,
                        message: `Maximum retry attempts reached. Stream fetch failed.`,
                        status: "maxRetriesReached",
                    });
                }
            } else {
                logger.info({
                    ...logContext,
                    message: "Stream fetched successfully.",
                    status: "streamFetched",
                });
                const schedules = await scheduleFeature.getScheduleByBroadcaster(event.broadcaster_user_id);
                for (const schedule of schedules) {
                    if (scheduleFeature.matchesCriteria(schedule, streamFetched)) {
                        logger.info({
                            broadcasterID: event.broadcaster_user_id,
                            message: "Download initiated for matching schedule.",
                            action: "downloadInitiated",
                        });
                        const jobDetails = downloadFeature.getDownloadJobDetail(
                            streamFetched,
                            "system",
                            streamFetched.channel,
                            ""
                        );
                        // TODO map the quality of the schedule
                        await downloadFeature.handleDownload(jobDetails, event.broadcaster_user_id);
                        break;
                    }
                }
            }
        } catch (error) {
            if (retryCount < 5) {
                setTimeout(() => fetchStream(retryCount + 1), 120000);
            } else {
                logger.error({
                    ...logContext,
                    message: "Maximum retries reached after error. Stream remains OFFLINE.",
                    status: "maxRetriesAfterError",
                });
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
}

export const handleRevocation = (notification: any) => {
    logger.info("Received a revocation:");
    logger.info(JSON.stringify(notification, null, 2));
    logger.info(`${notification.subscription.type} notifications revoked!`);
    logger.info(`Reason: ${notification.subscription.status}`);
    logger.info(`Condition: ${JSON.stringify(notification.subscription.condition, null, 4)}`);
};
