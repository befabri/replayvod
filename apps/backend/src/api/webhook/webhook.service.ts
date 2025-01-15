import { env } from "../../app";
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
} from "../../models/model.twitch";
import { transformWebhookEvent } from "./webhook.dto";
import { ChannelRepository } from "../channel/channel.repository";
import { DownloadService } from "../download/download.service";
import { ScheduleRepository } from "../schedule/schedule.repository";
import { PrismaClient } from "@prisma/client";
const logger = rootLogger.child({ domain: "webhook", service: "service" });

export class WebhookService {
    constructor(
        private db: PrismaClient,
        private channelRepository: ChannelRepository,
        private downloadService: DownloadService,
        private scheduleRepository: ScheduleRepository
    ) {}

    getWebhook = async (id: string) => {
        // return prisma.event.findUnique({
        //     where: { id: id },
        // });
        return id;
    };

    getAllWebhooks = async () => {
        return this.db.webhookEvent.findMany();
    };

    private createWebhookEvent = async (eventType: SubscriptionType, event: TwitchEvent) => {
        try {
            const webhookEvent = transformWebhookEvent(eventType, event.broadcaster_user_id);
            if (!webhookEvent) {
                return;
            }
            await this.db.webhookEvent.create({
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

    getHMACSecret = () => {
        return env.hmacSecret;
    };

    getCallbackUrlWebhook = (): string => {
        return env.callbackUrlWebhook;
    };

    isTwitchNotificationEvent = (
        notification: TwitchNotificationBody
    ): notification is TwitchNotificationEvent => {
        return "event" in notification;
    };

    isTwitchNotificationChallenge = (
        notification: TwitchNotificationBody
    ): notification is TwitchNotificationChallenge => {
        return "challenge" in notification;
    };

    isTwitchNotificationRevocation = (
        notification: TwitchNotificationBody
    ): notification is TwitchNotificationRevocation => {
        return !("event" in notification) && !("challenge" in notification);
    };

    async handleChannelUpdate(notification: TwitchNotificationEvent) {
        logger.info(`Handling channel update for broadcaster ID: ${notification.event.broadcaster_user_id}`);
    }

    async handleStreamOnline(notification: TwitchNotificationEvent) {
        const event = notification.event as StreamOnlineEvent;
        logger.info({
            broadcasterID: event.broadcaster_user_id,
            message: `Stream online handling initiated.`,
            action: "streamOnlineHandlingStart",
        });
        await this.createWebhookEvent(notification.subscription.type, event);
        const stream = await this.channelRepository.fetchStreamWithRetries(event.broadcaster_user_id);
        if (stream) {
            const schedules = await this.scheduleRepository.getScheduleMatch(stream, event.broadcaster_user_id);
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

                const jobDetails = this.downloadService.getDownloadJobDetail(
                    stream,
                    schedules.map((schedule) => schedule.requestedBy),
                    stream.channel,
                    highestResolution
                );
                await this.downloadService.handleDownload(jobDetails, event.broadcaster_user_id);
            }
        }
    }

    async handleStreamOffline(notification: TwitchNotificationEvent) {
        const event = notification.event as StreamOfflineEvent;
        logger.info(`Handling stream offline for broadcaster ID: ${event.broadcaster_user_id}`);
        await this.createWebhookEvent(notification.subscription.type, event);
        const lastStream = await this.channelRepository.getLastActiveStreamByBroadcaster(
            event.broadcaster_user_id
        );
        if (!lastStream) {
            logger.error(`Stream not found in handleStreamOffline for ${event.broadcaster_user_id}`);
            return;
        }
        await this.channelRepository.updateStreamEnded(lastStream.id);
    }

    async handleNotification(notification: TwitchNotificationBody) {
        logger.info(`Handling generic notification for subscription type: ${notification.subscription.type}`);
    }

    handleRevocation = (notification: any) => {
        logger.info("Received a revocation:");
        logger.info(JSON.stringify(notification, null, 2));
        logger.info(`${notification.subscription.type} notifications revoked!`);
        logger.info(`Reason: ${notification.subscription.status}`);
        logger.info(`Condition: ${JSON.stringify(notification.subscription.condition, null, 4)}`);
    };
}
