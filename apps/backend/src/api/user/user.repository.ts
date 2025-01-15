import { FastifyRequest } from "fastify";
import { logger as rootLogger } from "../../app";
import { UserSession } from "../../models/model.user";
import { transformSessionUser } from "./user.dto";
import { TwitchUserData } from "../../models/model.twitch";
import { ChannelRepository } from "../channel/channel.repository";
import { TwitchService } from "../../services/service.twitch";
import { CacheService, cacheType } from "../../services/service.cache";
import { PrismaClient } from "@prisma/client";
const logger = rootLogger.child({ domain: "user", service: "repository" });

export class UserRepository {
    constructor(
        private db: PrismaClient,
        private channelRepository: ChannelRepository,
        private twitchService: TwitchService,
        private cacheService: CacheService
    ) {}

    getUserIdFromSession = (req: FastifyRequest): string | null => {
        const userSession = req.session?.user as UserSession | undefined;
        if (userSession && userSession.twitchUserID && userSession.twitchUserID) {
            return userSession.twitchUserID;
        }
        return null;
    };

    getUserAccessTokenFromSession = (req: FastifyRequest): string | null => {
        const userSession = req.session?.user as UserSession | undefined;
        if (userSession && userSession.twitchToken.access_token) {
            return userSession.twitchToken.access_token;
        }
        return null;
    };

    updateUserDetail = async (userData: TwitchUserData) => {
        const user = await transformSessionUser(userData);
        if (user) {
            try {
                await this.db.user.upsert({
                    where: { userId: user.userId },
                    update: user,
                    create: user,
                });
            } catch (error) {
                logger.error("Error updating/inserting user: %s", error);
            }
        }
        return user;
    };

    private getUserFollowedStreamsLastFetch = async (userId: string) => {
        const fetchLog = await this.cacheService.getLastFetch({
            fetchType: cacheType.FOLLOWED_STREAMS,
            userId: userId,
        });

        if (fetchLog && this.cacheService.isCacheExpire(fetchLog.fetchedAt)) {
            return this.db.stream.findMany({
                where: {
                    fetchId: fetchLog.id,
                },
            });
        }
        return null;
    };

    getUserFollowedStreams = async (userId: string, accessToken: string) => {
        try {
            const lastUserFollowedStreamsFetch = await this.getUserFollowedStreamsLastFetch(userId);
            if (lastUserFollowedStreamsFetch) {
                return lastUserFollowedStreamsFetch;
            }
            const followedStreams = await this.twitchService.getAllFollowedStreams(userId, accessToken);
            if (!followedStreams) {
                return null;
            }
            const newFetchLog = await this.cacheService.createFetch({
                fetchType: cacheType.FOLLOWED_STREAMS,
                userId: userId,
            });
            for (let { stream, tags, category, title } of followedStreams) {
                await this.channelRepository.getChannel(stream.broadcasterId);
                await this.channelRepository.createStreamEntry({
                    fetchId: newFetchLog.id,
                    stream: stream,
                    tags: tags,
                    category: category,
                    title: title,
                });
            }
            return followedStreams;
        } catch (error) {
            logger.error(`Error fetching followed streams: ${error}`);
            throw new Error("Error fetching followed streams");
        }
    };

    isChannelFollowed = async (broadcasterId: string): Promise<boolean> => {
        const followedChannel = await this.db.userFollowedChannels.findFirst({
            where: {
                broadcasterId: broadcasterId,
            },
        });
        return !!followedChannel;
    };

    getAllFollowedChannelsDb = async () => {
        try {
            const followedChannelsRelations = await this.db.userFollowedChannels.findMany({
                include: {
                    channel: true,
                },
                orderBy: {
                    channel: {
                        broadcasterName: "asc",
                    },
                },
            });
            return followedChannelsRelations.map((relation) => relation.channel);
        } catch (error) {
            logger.error("Error getting followed channels from Db:", error);
            throw new Error("Error getting followed channels from Db");
        }
    };

    getUserFollowedChannelsDb = async (userId: string) => {
        try {
            const followedChannelsRelations = await this.db.userFollowedChannels.findMany({
                where: {
                    userId: userId,
                },
                include: {
                    channel: true,
                },
                orderBy: {
                    channel: {
                        broadcasterName: "asc",
                    },
                },
            });
            return followedChannelsRelations.map((relation) => relation.channel);
        } catch (error) {
            logger.error("Error getting user followed channels from Db:", error);
            throw new Error("Error getting user followed channels from Db");
        }
    };

    getFollowedChannelBroadcasterIds = async (): Promise<string[]> => {
        const channels = await this.getAllFollowedChannelsDb();
        return channels.map((channel) => channel.broadcasterId);
    };

    private getUserFollowedChannelsLastFetch = async (userId: string) => {
        const fetchLog = await this.cacheService.getLastFetch({
            fetchType: cacheType.FOLLOWED_CHANNELS,
            userId: userId,
        });

        if (fetchLog && this.cacheService.isCacheExpire(fetchLog.fetchedAt)) {
            return await this.getUserFollowedChannelsDb(userId);
        }
        return null;
    };

    getUserFollowedChannels = async (userId: string, accessToken: string) => {
        try {
            const lastUserFollowedChannelsFetch = await this.getUserFollowedChannelsLastFetch(userId);
            if (lastUserFollowedChannelsFetch) {
                return lastUserFollowedChannelsFetch;
            }
            await this.cacheService.createFetch({
                fetchType: cacheType.FOLLOWED_CHANNELS,
                userId: userId,
            });
            const followedChannels = await this.twitchService.getAllFollowedChannels(userId, accessToken);
            if (!followedChannels) {
                return null;
            }
            try {
                await this.db.userFollowedChannels.updateMany({
                    where: {
                        userId: userId,
                    },
                    data: {
                        followed: false,
                    },
                });
                for (const channel of followedChannels) {
                    const channelData = await this.channelRepository.getChannel(channel.broadcasterId);
                    if (!channelData) {
                        logger.error("Channel not found: %s", channel.broadcasterId);
                        continue;
                    }
                    await this.db.userFollowedChannels.upsert({
                        where: {
                            broadcasterId_userId: {
                                broadcasterId: channel.broadcasterId,
                                userId: userId,
                            },
                        },
                        update: {
                            ...channel,
                            followed: true,
                        },
                        create: {
                            ...channel,
                            followed: true,
                        },
                    });
                }
            } catch (error) {
                logger.error("Error updating channel user followed: %s", error);
            }
            return followedChannels;
        } catch (error) {
            logger.error("Error fetching followed channels from Twitch API: %s", error);
            throw new Error("Error fetching followed channels from Twitch API");
        }
    };
}
