import { logger as rootLogger } from "../../app";
import { Channel, PrismaClient } from "@prisma/client";
import { CreateStreamEntry } from "../../types/sharedTypes";
import { PrismaClientKnownRequestError } from "@prisma/client/runtime/library";
import { delay } from "../../utils/utils";
import { StreamStatus } from "../../models/model.twitch";
import { CategoryRepository } from "../category/category.repository";
import { StreamDTO } from "./channel.dto";
import { CacheService, cacheType } from "../../services/service.cache";
import { TagService } from "../../services/service.tag";
import { TitleService } from "../../services/service.title";
import { TwitchService } from "../../services/service.twitch";
const logger = rootLogger.child({ domain: "channel", service: "repository" });

export class ChannelRepository {
    constructor(
        private db: PrismaClient,
        private categoryRepository: CategoryRepository,
        private cacheService: CacheService,
        private titleService: TitleService,
        private tagService: TagService,
        private twitchService: TwitchService
    ) {}

    getChannelDb = async (broadcasterId: string): Promise<Channel | null> => {
        return this.db.channel.findUnique({ where: { broadcasterId: broadcasterId } });
    };

    getChannelDbByName = async (loginName: string): Promise<Channel | null> => {
        return this.db.channel.findUnique({ where: { broadcasterLogin: loginName.toLowerCase() } });
    };

    getMultipleChannelDB = async (broadcasterIds: string[]): Promise<Channel[] | null> => {
        const channels = await this.db.channel.findMany({
            where: {
                broadcasterId: {
                    in: broadcasterIds,
                },
            },
        });
        return channels;
    };

    createChannel = async (channelData: Channel): Promise<Channel | null> => {
        try {
            const channel = await this.db.channel.create({
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

    getChannel = async (broadcasterId: string): Promise<Channel | null> => {
        try {
            let channel = await this.getChannelDb(broadcasterId);
            if (!channel) {
                channel = await this.updateChannel(broadcasterId);
            }
            return channel;
        } catch (error) {
            logger.error("Error getting channel: %s", error);
            return null;
        }
    };

    updateChannel = async (broadcasterId: string): Promise<Channel | null> => {
        try {
            const channelData = await this.twitchService.getUser(broadcasterId);
            if (!channelData) {
                return null;
            }
            const channel = await this.createChannel(channelData);
            return channel;
        } catch (error) {
            logger.error("Error updating channel: %s", error);
            return null;
        }
    };

    getChannelByName = async (login: string): Promise<Channel | null> => {
        try {
            let channel = await this.getChannelDbByName(login);
            if (!channel) {
                const channelData = await this.twitchService.getUserByLogin(login);
                if (!channelData) {
                    return null;
                }
                channel = await this.createChannel(channelData);
            }
            return channel;
        } catch (error) {
            logger.error("Error ensureChannel: %s", error);
            return null;
        }
    };

    channelExists = async (broadcasterId: string): Promise<boolean> => {
        const channel = await this.getChannel(broadcasterId);
        return !!channel;
    };

    fetchStreamWithRetries = async (broadcasterId: string, maxRetries = 5) => {
        for (let retryCount = 0; retryCount <= maxRetries; retryCount++) {
            const logContext = { broadcasterId, retryAttempt: retryCount, action: "fetchStreamAttempt" };
            try {
                const streamFetched = await this.getChannelStream(broadcasterId, "system");
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

    getChannelStream = async (broadcasterId: string, userId: string): Promise<StreamDTO | null> => {
        try {
            if (!(await this.channelExists(broadcasterId))) {
                return null;
            }
            const stream = await this.twitchService.getStreamByUserId(broadcasterId);
            if (!stream || stream === StreamStatus.OFFLINE) {
                return null;
            }
            const newFetchLog = await this.cacheService.createFetch({
                fetchType: cacheType.STREAM,
                userId: userId,
                broadcasterId: broadcasterId,
            });
            await this.createStreamEntry({
                fetchId: newFetchLog.id,
                stream: stream.stream,
                tags: stream.tags,
                category: stream.category,
                title: stream.title,
            });
            return this.getStreamByFetchId(newFetchLog.id);
        } catch (error) {
            logger.error(`Error fetching stream: ${error}`);
            throw new Error("Error fetching stream");
        }
    };

    updateStreamEnded = async (streamId: string) => {
        const result = await this.db.stream.update({
            where: {
                id: streamId,
            },
            data: {
                endedAt: new Date(),
            },
        });
        return result;
    };

    getLastActiveStreamByBroadcaster = async (broadcasterId: string) => {
        const lastActiveStream = await this.db.stream.findMany({
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

    createStreamEntry = async ({ fetchId, stream, tags, category, title }: CreateStreamEntry) => {
        try {
            await this.db.$transaction(async (tx) => {
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
                    await this.titleService.createTitle(title);
                    await this.titleService.createStreamTitle(stream.id, title.name, tx as PrismaClient);
                }
                if (category) {
                    await this.categoryRepository.createCategory(category);
                    await this.categoryRepository.createStreamCategory(stream.id, category.id, tx as PrismaClient);
                }
                if (tags.length > 0) {
                    await this.tagService.createMultipleTags(tags);
                    await this.tagService.createMultipleStreamTags(
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

    getStreamByFetchId = async (fetchId: string): Promise<StreamDTO | null> => {
        const stream = await this.db.stream.findFirst({
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
            categories: stream.categories.map((c) => c.category),
            tags: stream.tags.map((t) => t.tag),
        };
    };

    getStreamLastFetch = async (userId: string, broadcasterId: string) => {
        const fetchLog = await this.cacheService.getLastFetch({
            fetchType: cacheType.STREAM,
            userId: userId,
            broadcasterId: broadcasterId,
        });
        if (fetchLog && this.cacheService.isCacheExpire(fetchLog.fetchedAt)) {
            return this.getStreamByFetchId(fetchLog.id);
        }
        return null;
    };

    getLastLive = async () => {
        return this.db.stream.findMany({
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
}
