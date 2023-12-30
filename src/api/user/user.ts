import { FastifyRequest } from "fastify";
import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { TwitchUserData, UserSession } from "../../models/userModel";
import { transformSessionUser } from "./user.DTO";
import { twitchFeature } from "../twitch";
import { channelFeature } from "../channel";
import { cacheService } from "../../services";
const logger = rootLogger.child({ domain: "auth", service: "userService" });

export const getUserIdFromSession = (req: FastifyRequest): string | null => {
    const userSession = req.session?.user as UserSession | undefined;
    if (userSession && userSession.twitchUserData && userSession.twitchUserData.id) {
        return userSession.twitchUserData.id;
    }
    return null;
};

export const getUserAccessTokenFromSession = (req: FastifyRequest): string | null => {
    const userSession = req.session?.user as UserSession | undefined;
    if (userSession && userSession.twitchToken.access_token) {
        return userSession.twitchToken.access_token;
    }
    return null;
};

export const updateUserDetail = async (userData: TwitchUserData) => {
    const user = await transformSessionUser(userData);
    if (user) {
        try {
            await prisma.user.upsert({
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

const getUserFollowedStreamsLastFetch = async (userId: string) => {
    const fetchLog = await cacheService.getLastFetch({
        fetchType: cacheService.cacheType.FOLLOWED_STREAMS,
        userId: userId,
    });

    if (fetchLog && cacheService.isCacheExpire(fetchLog.fetchedAt)) {
        return prisma.stream.findMany({
            where: {
                fetchId: fetchLog.id,
            },
        });
    }
    return null;
};

export const getUserFollowedStreams = async (userId: string, accessToken: string) => {
    try {
        const lastUserFollowedStreamsFetch = await getUserFollowedStreamsLastFetch(userId);
        if (lastUserFollowedStreamsFetch) {
            return lastUserFollowedStreamsFetch;
        }
        const followedStreams = await twitchFeature.getAllFollowedStreams(userId, accessToken);
        if (!followedStreams) {
            return null;
        }
        const newFetchLog = await cacheService.createFetch({
            fetchType: cacheService.cacheType.FOLLOWED_STREAMS,
            userId: userId,
        });
        for (let { stream, tags, category, title } of followedStreams) {
            await channelFeature.getChannel(stream.broadcasterId);
            await channelFeature.createStreamEntry({
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

export const isChannelFollowed = async (broadcasterId: string): Promise<boolean> => {
    const followedChannel = await prisma.userFollowedChannels.findFirst({
        where: {
            broadcasterId: broadcasterId,
        },
    });
    return !!followedChannel;
};

export const getAllFollowedChannelsDb = async () => {
    try {
        const followedChannelsRelations = await prisma.userFollowedChannels.findMany({
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

export const getUserFollowedChannelsDb = async (userId: string) => {
    try {
        const followedChannelsRelations = await prisma.userFollowedChannels.findMany({
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

export const getFollowedChannelBroadcasterIds = async (): Promise<string[]> => {
    const channels = await getAllFollowedChannelsDb();
    return channels.map((channel) => channel.broadcasterId);
};

const getUserFollowedChannelsLastFetch = async (userId: string) => {
    const fetchLog = await cacheService.getLastFetch({
        fetchType: cacheService.cacheType.FOLLOWED_CHANNELS,
        userId: userId,
    });

    if (fetchLog && cacheService.isCacheExpire(fetchLog.fetchedAt)) {
        return await getUserFollowedChannelsDb(userId);
    }
    return null;
};

export const getUserFollowedChannels = async (userId: string, accessToken: string) => {
    try {
        logger.info("userId %s", userId);
        logger.info("accessToken %s", accessToken);
        const lastUserFollowedChannelsFetch = await getUserFollowedChannelsLastFetch(userId);
        if (lastUserFollowedChannelsFetch) {
            return lastUserFollowedChannelsFetch;
        }
        await cacheService.createFetch({
            fetchType: cacheService.cacheType.FOLLOWED_CHANNELS,
            userId: userId,
        });
        const followedChannels = await twitchFeature.getAllFollowedChannels(userId, accessToken);
        if (!followedChannels) {
            return null;
        }
        try {
            await prisma.userFollowedChannels.updateMany({
                where: {
                    userId: userId,
                },
                data: {
                    followed: false,
                },
            });
            for (const channel of followedChannels) {
                const channelData = await channelFeature.getChannel(channel.broadcasterId);
                if (!channelData) {
                    logger.info("Channel not found: %s", channel.broadcasterId);
                    continue;
                }
                await prisma.userFollowedChannels.upsert({
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
