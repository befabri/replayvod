import { logger as rootLogger } from "../../app";
import { SubscriptionType } from "../../models/model.twitch";
import { EventSubDTO } from "./eventSub.dto";
import { UserRepository } from "../user/user.repository";
import { WebhookService } from "../webhook/webhook.service";
import { ChannelRepository } from "../channel/channel.repository";
import { TwitchService } from "../../services/service.twitch";
import { CacheService, cacheType } from "../../services/service.cache";
import { PrismaClient } from "@prisma/client";
const logger = rootLogger.child({ domain: "event-sub", service: "service" });

export class EventSubService {
    constructor(
        private db: PrismaClient,
        private userRepository: UserRepository,
        private webhookService: WebhookService,
        private channelRepository: ChannelRepository,
        private twitchService: TwitchService,
        private cacheService: CacheService
    ) {}

    subToAllChannelFollowed = async () => {
        const broadcasterIds = await this.userRepository.getFollowedChannelBroadcasterIds();
        let responses = [];
        for (const broadcasterId of broadcasterIds) {
            try {
                const respOnline = await this.subscribeToStreamOnline(broadcasterId);
                const respOffline = await this.subscribeToStreamOffline(broadcasterId);
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

    subscribeToStreamOnline = async (userId: string) => {
        return await this.twitchService.createEventSub(
            SubscriptionType.STREAM_ONLINE,
            "1",
            { broadcaster_user_id: userId },
            {
                method: "webhook",
                callback: this.webhookService.getCallbackUrlWebhook(),
                secret: this.webhookService.getHMACSecret(),
            }
        );
    };

    subscribeToStreamOffline = async (userId: string) => {
        return await this.twitchService.createEventSub(
            SubscriptionType.STREAM_OFFLINE,
            "1",
            { broadcaster_user_id: userId },
            {
                method: "webhook",
                callback: this.webhookService.getCallbackUrlWebhook(),
                secret: this.webhookService.getHMACSecret(),
            }
        );
    };

    private getEventSubLastFetch = async (userId: string): Promise<EventSubDTO | null> => {
        const fetchLog = await this.cacheService.getLastFetch({
            fetchType: cacheType.EVENT_SUB,
            userId: userId,
        });

        if (fetchLog && this.cacheService.isCacheExpire(fetchLog.fetchedAt)) {
            const eventSub = await this.db.eventSub.findUnique({
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

    getEventSub = async (userId: string): Promise<EventSubDTO> => {
        const lastEventSubFetch = await this.getEventSubLastFetch(userId);
        if (lastEventSubFetch) {
            return lastEventSubFetch;
        }
        const eventSub = await this.twitchService.getEventSub();
        if (eventSub && "meta" in eventSub) {
            if (eventSub.meta.total === 0) {
                return { data: null, message: "There is no EventSub subscription" };
            }

            const newFetchLog = await this.cacheService.createFetch({
                fetchType: cacheType.EVENT_SUB,
                userId: userId,
            });

            const createdEventSub = await this.db.eventSub.create({
                data: {
                    userId: userId,
                    fetchId: newFetchLog.id,
                    total: eventSub.meta.total,
                    totalCost: eventSub.meta.total_cost,
                    maxTotalCost: eventSub.meta.max_total_cost,
                },
            });

            const processPromises = eventSub.subscriptions.map(async (sub) => {
                const broadcasterExists = await this.channelRepository.channelExists(sub.broadcasterId);
                if (!broadcasterExists) {
                    logger.error(`Broadcaster with ID ${sub.broadcasterId} does not exist in the database.`);
                    await this.channelRepository.updateChannel(sub.broadcasterId);
                }

                await this.db.$transaction([
                    this.db.subscription.upsert({
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
                    this.db.subscriptionEventSub.create({
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
}
