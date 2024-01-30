import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { WebhookEvent } from "@prisma/client";
import { webhookFeature } from "../webhook";
import { channelFeature } from "../channel";
import { cacheService, twitchService } from "../../services";
import { STREAM_OFFLINE, STREAM_ONLINE } from "../../constants/twitchConstants";
import { userFeature } from "../user";
const logger = rootLogger.child({ domain: "webhook", service: "eventSubService" });

export const subToAllChannelFollowed = async () => {
    const broadcasterIds = await userFeature.getFollowedChannelBroadcasterIds();
    let responses = [];
    for (const broadcasterId of broadcasterIds) {
        try {
            const respOnline = await subscribeToStreamOnline(broadcasterId);
            const respOffline = await subscribeToStreamOffline(broadcasterId);
            responses.push({ channel: broadcasterId, online: respOnline, offline: respOffline });
        } catch (error) {
            if (error instanceof Error) {
                responses.push({ channel: broadcasterId, error: error.message });
            } else {
                responses.push({ channel: broadcasterId, error: error });
            }
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
        STREAM_ONLINE,
        "1",
        { broadcaster_user_id: userId },
        {
            method: "webhook",
            callback: webhookFeature.getCallbackUrlWebhook(),
            secret: webhookFeature.getSecret(),
        }
    );
};

export const subscribeToStreamOffline = async (userId: string) => {
    return await twitchService.createEventSub(
        STREAM_OFFLINE,
        "1",
        { broadcaster_user_id: userId },
        {
            method: "webhook",
            callback: webhookFeature.getCallbackUrlWebhook(),
            secret: webhookFeature.getSecret(),
        }
    );
};

export const getEventSubLastFetch = async (userId: string) => {
    const fetchLog = await cacheService.getLastFetch({
        fetchType: cacheService.cacheType.EVENT_SUB,
        userId: userId,
    });

    if (fetchLog && cacheService.isCacheExpire(fetchLog.fetchedAt)) {
        const eventSubs = await prisma.eventSub.findMany({
            where: {
                fetchId: fetchLog.id,
            },
            include: {
                subscriptions: {
                    include: {
                        subscription: true,
                    },
                },
            },
        });

        const subscriptions = eventSubs.flatMap((eventSub) =>
            eventSub.subscriptions.map((subEvent) => subEvent.subscription)
        );
        return subscriptions;
    }
    return null;
};

export const getEventSub = async (userId: string) => {
    const lastEventSubFetch = await getEventSubLastFetch(userId);
    if (lastEventSubFetch) {
        return { data: lastEventSubFetch, message: "EventSub from cache" };
    }
    const eventSub = await twitchService.getEventSub();
    if (!eventSub) {
        return { data: null, message: "Failed to get EventSub from Twitch" };
    }
    const newFetchLog = await cacheService.createFetch({
        fetchType: cacheService.cacheType.EVENT_SUB,
        userId: userId,
    });
    const createdEventSub = await prisma.eventSub.create({
        data: {
            userId: userId,
            fetchId: newFetchLog.id,
        },
    });
    const processPromises = eventSub.subscriptions.map(async (sub) => {
        const broadcasterExists = await channelFeature.channelExists(sub.broadcasterId);
        if (!broadcasterExists) {
            logger.error(`Broadcaster with ID ${sub.broadcasterId} does not exist in the database.`);
            await channelFeature.updateChannel(sub.broadcasterId);
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

    return { data: eventSub.subscriptions, message: "EventSub subscriptions stored successfully." };
};

export const getTotalCost = async () => {
    const eventSubResult = await twitchService.getEventSub();
    if (eventSubResult && "meta" in eventSubResult) {
        const { meta } = eventSubResult;
        if (meta.total === 0) {
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
    }
    return { data: null, message: "Failed to retrieve EventSub information" };
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
