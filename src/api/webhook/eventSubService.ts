import { v4 as uuidv4 } from "uuid";
import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { webhookService } from ".";
import { channelService } from "../channel";
import { twitchService } from "../twitch";
import { WebhookEvent } from "@prisma/client";
const logger = rootLogger.child({ domain: "webhook", service: "eventSubService" });

export const subToAllChannelFollowed = async () => {
    const broadcasterIds = await channelService.getBroadcasterIds();
    let responses = [];
    for (const broadcasterId of broadcasterIds) {
        try {
            const respOnline = await subscribeToStreamOnline(broadcasterId);
            const respOffline = await subscribeToStreamOffline(broadcasterId);
            responses.push({ channel: broadcasterId, online: respOnline, offline: respOffline });
        } catch (error) {
            responses.push({ channel: broadcasterId, error: error.message });
        }
    }
    for (const resp of responses) {
        if (resp.error) {
            logger.error(`Channel ${resp.channel} - Error: ${resp.error}`);
        } else {
            logger.info(
                `Channel ${resp.channel} - Online Response: ${resp.online}, Offline Response: ${resp.offline}`
            );
        }
    }
};

export const subscribeToStreamOnline = async (userId: string) => {
    return await twitchService.createEventSub(
        "stream.online",
        "1",
        { broadcaster_user_id: userId },
        {
            method: "webhook",
            callback: webhookService.getCallbackUrlWebhook(),
            secret: webhookService.getSecret(),
        }
    );
};

export const subscribeToStreamOffline = async (userId: string) => {
    return await twitchService.createEventSub(
        "stream.offline",
        "1",
        { broadcaster_user_id: userId },
        {
            method: "webhook",
            callback: webhookService.getCallbackUrlWebhook(),
            secret: webhookService.getSecret(),
        }
    );
};

export const getEventSub = async (userId: string) => {
    const FIVE_MINUTES = 5 * 60 * 1000;

    const fetchLog = await prisma.fetchLog.findFirst({
        where: {
            userId: userId,
            fetchType: "eventSub",
        },
        orderBy: {
            fetchedAt: "desc",
        },
    });

    if (fetchLog && fetchLog.fetchedAt > new Date(Date.now() - FIVE_MINUTES)) {
        return prisma.eventSub.findMany({
            where: {
                fetchId: fetchLog.fetchId,
            },
        });
    }

    const fetchId = uuidv4();
    const { subscriptions } = await twitchService.getEventSub();

    await prisma.fetchLog.create({
        data: {
            userId: userId,
            fetchedAt: new Date(),
            fetchId: fetchId,
            fetchType: "eventSub",
        },
    });

    const createdEventSub = await prisma.eventSub.create({
        data: {
            userId: userId,
            fetchId: fetchId,
        },
    });

    const processPromises = subscriptions.map(async (sub) => {
        const broadcasterExists = await channelService.channelExists(sub.broadcasterId);
        if (!broadcasterExists) {
            logger.error(`Broadcaster with ID ${sub.broadcasterId} does not exist in the database.`);
            await channelService.updateChannelDetail(sub.broadcasterId);
        }

        await prisma.$transaction([
            prisma.subscription.upsert({
                where: {
                    broadcasterId_subscriptionType: {
                        broadcasterId: sub.broadcasterId,
                        subscriptionType: sub.subscriptionType,
                    },
                },
                update: {
                    status: sub.status,
                    cost: sub.cost,
                },
                create: {
                    id: sub.id,
                    status: sub.status,
                    subscriptionType: sub.subscriptionType,
                    broadcasterId: sub.broadcasterId,
                    createdAt: sub.createdAt,
                    cost: sub.cost,
                },
            }),
            prisma.subscriptionEventSub.create({
                data: {
                    eventSubId: createdEventSub.id,
                    subscriptionId: sub.id,
                },
            }),
        ]);
    });

    for (const promise of processPromises) {
        try {
            await promise;
        } catch (error) {
            logger.error(`Error processing subscription: %s`, error);
        }
    }

    return { data: subscriptions, message: "EventSub subscriptions stored successfully." };
};

export const getTotalCost = async () => {
    const { meta } = await twitchService.getEventSub();
    if (meta && meta.total === 0) {
        return { data: null, message: "There is no EventSub subscription" };
    }
    return {
        data: {
            total: meta.total,
            total_cost: meta.total_cost,
            max_total_cost: meta.max_total_cost,
        },
        message: "Total cost retrieved successfully",
    };
};

export const addWebhookEvent = async (event: Omit<WebhookEvent, "id">) => {
    try {
        await prisma.webhookEvent.create({
            data: {
                broadcasterId: event.broadcasterId,
                eventType: event.eventType,
                startedAt: event.startedAt,
                endAt: event.endAt,
            },
        });
    } catch (error) {
        throw error;
    }
};
