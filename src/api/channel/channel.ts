import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { Channel, PrismaClient } from "@prisma/client";
import { cacheService, tagService, titleService, twitchService } from "../../services";
import { CreateStreamEntry } from "../../types/sharedTypes";
import { PrismaClientKnownRequestError } from "@prisma/client/runtime/library";
import { categoryFeature } from "../category";
import { delay } from "../../utils/utils";
import { StreamDTO } from "./channel.DTO";
import { StreamStatus } from "../../models/twitchModel";
const logger = rootLogger.child({ domain: "channel", service: "channelService" });

export const getChannelDb = async (broadcasterId: string): Promise<Channel | null> => {
    return await prisma.channel.findUnique({ where: { broadcasterId: broadcasterId } });
};

export const getChannelDbByName = async (loginName: string): Promise<Channel | null> => {
    return await prisma.channel.findUnique({ where: { broadcasterLogin: loginName.toLowerCase() } });
};

export const getMultipleChannelDB = async (broadcasterIds: string[]): Promise<Channel[] | null> => {
    const channels = await prisma.channel.findMany({
        where: {
            broadcasterId: {
                in: broadcasterIds,
            },
        },
    });
    return channels;
};

export const createChannel = async (channelData: Channel): Promise<Channel | null> => {
    try {
        const channel = await prisma.channel.create({
            data: {
                ...channelData,
            },
        });
        return channel;
    } catch (error) {
        if (error instanceof PrismaClientKnownRequestError) {
            if (error.code === "P2002") {
                logger.error("Unique constraint failed, channel already exists: %s", error);
                return null;
            }
        }
        logger.error("Error creating channel: %s", error);
        return null;
    }
};

export const getChannel = async (broadcasterId: string): Promise<Channel | null> => {
    try {
        let channel = await getChannelDb(broadcasterId);
        if (!channel) {
            channel = await updateChannel(broadcasterId);
        }
        return channel;
    } catch (error) {
        logger.error("Error getting channel: %s", error);
        return null;
    }
};

export const updateChannel = async (broadcasterId: string): Promise<Channel | null> => {
    try {
        const channelData = await twitchService.getUser(broadcasterId);
        if (!channelData) {
            return null;
        }
        const channel = await createChannel(channelData);
        return channel;
    } catch (error) {
        logger.error("Error updating channel: %s", error);
        return null;
    }
};

export const getChannelByName = async (login: string): Promise<Channel | null> => {
    try {
        let channel = await getChannelDbByName(login);
        if (!channel) {
            const channelData = await twitchService.getUserByLogin(login);
            if (!channelData) {
                return null;
            }
            channel = await createChannel(channelData);
        }
        return channel;
    } catch (error) {
        logger.error("Error ensureChannel: %s", error);
        return null;
    }
};

export const channelExists = async (broadcasterId: string): Promise<boolean> => {
    const channel = await getChannel(broadcasterId);
    return !!channel;
};

export const fetchStreamWithRetries = async (broadcasterId: string, maxRetries = 5) => {
    for (let retryCount = 0; retryCount <= maxRetries; retryCount++) {
        const logContext = { broadcasterId, retryAttempt: retryCount, action: "fetchStreamAttempt" };
        try {
            const streamFetched = await getChannelStream(broadcasterId, "system");
            if (!streamFetched) {
                logger.warn({
                    ...logContext,
                    message: "Stream OFFLINE or not started. Retrying...",
                    status: "offlineOrNotStarted",
                });
                await delay(120000);
                continue;
            }
            logger.info({ ...logContext, message: "Stream fetched successfully.", status: "streamFetched" });
            return streamFetched;
        } catch (error) {
            logger.error({
                ...logContext,
                message: "Error fetching stream. Retrying...",
                error: error.toString(),
                status: "errorFetchingStream",
            });
            if (retryCount === maxRetries) {
                logger.error({
                    ...logContext,
                    message: "Maximum retries reached. Stream fetch failed.",
                    status: "maxRetriesReached",
                });
                return;
            } else {
                await delay(120000);
            }
        }
    }
};

export const getChannelStream = async (broadcasterId: string, userId: string): Promise<StreamDTO | null> => {
    try {
        if (!(await channelExists(broadcasterId))) {
            return null;
        }
        const stream = await twitchService.getStreamByUserId(broadcasterId);
        if (!stream || stream === StreamStatus.OFFLINE) {
            return null;
        }
        const newFetchLog = await cacheService.createFetch({
            fetchType: cacheService.cacheType.STREAM,
            userId: userId,
            broadcasterId: broadcasterId,
        });
        await createStreamEntry({
            fetchId: newFetchLog.id,
            stream: stream.stream,
            tags: stream.tags,
            category: stream.category,
            title: stream.title,
        });
        return await getStreamByFetchId(newFetchLog.id);
    } catch (error) {
        logger.error(`Error fetching stream: ${error}`);
        throw new Error("Error fetching stream");
    }
};

export const updateStreamEnded = async (streamId: string) => {
    const result = await prisma.stream.update({
        where: {
            id: streamId,
        },
        data: {
            endedAt: new Date(),
        },
    });
    return result;
};

export const getLastActiveStreamByBroadcaster = async (broadcasterId: string) => {
    const lastActiveStream = await prisma.stream.findMany({
        where: {
            broadcasterId: broadcasterId,
            endedAt: null,
        },
        orderBy: {
            startedAt: "desc",
        },
        take: 1,
    });
    return lastActiveStream.length > 0 ? lastActiveStream[0] : null;
};

export const createStreamEntry = async ({ fetchId, stream, tags, category, title }: CreateStreamEntry) => {
    try {
        await prisma.$transaction(async (tx) => {
            const streamInserted = await tx.stream.upsert({
                where: { id: stream.id },
                update: {
                    ...stream,
                    fetchId: fetchId,
                },
                create: {
                    ...stream,
                    fetchId: fetchId,
                },
            });
            if (title) {
                await titleService.createTitle(title);
                await titleService.createStreamTitle(stream.id, title.name, tx as PrismaClient);
            }
            if (category) {
                await categoryFeature.createCategory(category);
                await categoryFeature.createStreamCategory(stream.id, category.id, tx as PrismaClient);
            }
            if (tags.length > 0) {
                await tagService.createMultipleTags(tags);
                await tagService.createMultipleStreamTags(
                    tags.map((tag: { name: string }) => ({ tagId: tag.name })),
                    stream.id,
                    tx as PrismaClient
                );
            }
            return streamInserted;
        });
    } catch (error) {
        logger.error(`Error creating stream entry: ${error}`);
        // throw new Error("Error creating stream entry");
        return null;
    }
};

export const getStreamByFetchId = async (fetchId: string): Promise<StreamDTO | null> => {
    const stream = await prisma.stream.findFirst({
        where: {
            fetchId: fetchId,
        },
        include: {
            channel: true,
            fetchLog: true,
            tags: {
                include: {
                    tag: true,
                },
            },
            videos: true,
            categories: {
                include: {
                    category: true,
                },
            },
            titles: true,
        },
    });

    if (!stream) {
        return null;
    }

    return {
        ...stream,
        categories: stream.categories.map((c) => c.category.name),
        tags: stream.tags.map((t) => t.tag.name),
    };
};

export const getStreamLastFetch = async (userId: string, broadcasterId: string) => {
    const fetchLog = await cacheService.getLastFetch({
        fetchType: cacheService.cacheType.STREAM,
        userId: userId,
        broadcasterId: broadcasterId,
    });
    if (fetchLog && cacheService.isCacheExpire(fetchLog.fetchedAt)) {
        return await getStreamByFetchId(fetchLog.id);
    }
    return null;
};

export const getLastLive = async () => {
    return prisma.stream.findMany({
        where: {
            endedAt: {
                not: null,
            },
        },
        orderBy: {
            endedAt: "desc",
        },
        take: 10,
        include: {
            channel: true,
        },
    });
};
