import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { webhookFeature } from "../webhook";
import { channelFeature } from "../channel";
import { cacheService, twitchService } from "../../services";
import { userFeature } from "../user";
import { SubscriptionType } from "../../models/twitchModel";
import { EventSubDTO } from "./eventSub.DTO";
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
        SubscriptionType.STREAM_ONLINE,
        "1",
        { broadcaster_user_id: userId },
        {
            method: "webhook",
            callback: webhookFeature.getCallbackUrlWebhook(),
            secret: webhookFeature.getHMACSecret(),
        }
    );
};

export const subscribeToStreamOffline = async (userId: string) => {
    return await twitchService.createEventSub(
        SubscriptionType.STREAM_OFFLINE,
        "1",
        { broadcaster_user_id: userId },
        {
            method: "webhook",
            callback: webhookFeature.getCallbackUrlWebhook(),
            secret: webhookFeature.getHMACSecret(),
        }
    );
};

export const getEventSubLastFetch = async (userId: string): Promise<EventSubDTO | null> => {
    const fetchLog = await cacheService.getLastFetch({
        fetchType: cacheService.cacheType.EVENT_SUB,
        userId: userId,
    });

    if (fetchLog && cacheService.isCacheExpire(fetchLog.fetchedAt)) {
        const eventSub = await prisma.eventSub.findUnique({
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
        if (eventSub && eventSub.subscriptions.length > 0) {
            const subscriptions = eventSub.subscriptions.flatMap((subEvent) => subEvent.subscription);
            return {
                data: {
                    cost: {
                        total: eventSub.total,
                        total_cost: eventSub.totalCost,
                        max_total_cost: eventSub.maxTotalCost,
                    },
                    list: subscriptions.map((sub) => ({
                        id: sub.id,
                        status: sub.status,
                        subscriptionType: sub.subscriptionType,
                        broadcasterId: sub.broadcasterId,
                        createdAt: sub.createdAt,
                        cost: sub.cost,
                    })),
                },
                message: "EventSub from cache",
            };
        }
    }
    return null;
};

export const getEventSub = async (userId: string): Promise<EventSubDTO> => {
    const lastEventSubFetch = await getEventSubLastFetch(userId);
    if (lastEventSubFetch) {
        return lastEventSubFetch;
    }
    const eventSub = await twitchService.getEventSub();
    if (eventSub && "meta" in eventSub) {
        if (eventSub.meta.total === 0) {
            return { data: null, message: "There is no EventSub subscription" };
        }

        const newFetchLog = await cacheService.createFetch({
            fetchType: cacheService.cacheType.EVENT_SUB,
            userId: userId,
        });

        const createdEventSub = await prisma.eventSub.create({
            data: {
                userId: userId,
                fetchId: newFetchLog.id,
                total: eventSub.meta.total,
                totalCost: eventSub.meta.total_cost,
                maxTotalCost: eventSub.meta.max_total_cost,
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

        return {
            data: {
                cost: {
                    total: eventSub.meta.total,
                    total_cost: eventSub.meta.total_cost,
                    max_total_cost: eventSub.meta.max_total_cost,
                },
                list: eventSub.subscriptions,
            },
            message: "EventSub subscriptions stored successfully.",
        };
    }
    return { data: null, message: "Failed to retrieve EventSub information" };
};
