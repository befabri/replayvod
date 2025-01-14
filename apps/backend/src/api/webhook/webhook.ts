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
} from "../../models/twitchModel";
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

export const getHMACSecret = () => {
    return env.hmacSecret;
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
    const stream = await channelFeature.fetchStreamWithRetries(event.broadcaster_user_id);
    if (stream) {
        const schedules = await scheduleFeature.getScheduleMatch(stream, event.broadcaster_user_id);
        if (schedules.length > 0) {
            logger.info({
                broadcasterId: event.broadcaster_user_id,
                message: "Download initiated for matching schedule.",
                action: "downloadInitiated",
            });

            const highestResolution = schedules.reduce((acc, schedule) => {
                const currentRes = parseInt(schedule.quality);
                const accRes = parseInt(acc);
                return currentRes > accRes ? schedule.quality : acc;
            }, "0");

            const jobDetails = downloadFeature.getDownloadJobDetail(
                stream,
                schedules.map((schedule) => schedule.requestedBy),
                stream.channel,
                highestResolution
            );
            await downloadFeature.handleDownload(jobDetails, event.broadcaster_user_id);
        }
    }
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
