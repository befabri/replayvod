import { v4 as uuidv4 } from "uuid";
import { logger as rootLogger } from "@app";
import { prisma } from "@server";
import { Channel } from "@prisma/client";
import { tagService, titleService } from "@services";
import * as twitchService from "@api/twitch";
import * as categoryService from "@api/category";
import { StreamWithRelations } from "@sharedTypes";
const logger = rootLogger.child({ domain: "channel", service: "channelService" });

export const getUserFollowedStreams = async (userId: string, accessToken: string) => {
    try {
        const fetchLog = await prisma.fetchLog.findFirst({
            where: {
                userId: userId,
                fetchType: "followedStreams",
            },
            orderBy: {
                fetchedAt: "desc",
            },
        });
        if (fetchLog && fetchLog.fetchedAt > new Date(Date.now() - 5 * 60 * 1000)) {
            return prisma.stream.findMany({
                where: {
                    fetchId: fetchLog.fetchId,
                },
            });
        }
        const fetchId = uuidv4();
        const followedStreams = await twitchService.getAllFollowedStreams(userId, accessToken);
        await prisma.fetchLog.create({
            data: {
                userId: userId,
                fetchedAt: new Date(),
                fetchId: fetchId,
                fetchType: "followedStreams",
            },
        });
        for (let { stream, tags, category, title } of followedStreams) {
            await createStreamEntry(stream, tags, category, title, fetchId);
        }
        return followedStreams;
    } catch (error) {
        logger.error(`Error fetching followed streams: ${error}`);
        throw new Error("Error fetching followed streams");
    }
};

export const getStream = async (
    broadcasterId: string,
    userId: string
): Promise<StreamWithRelations | undefined> => {
    try {
        const fetchLog = await prisma.fetchLog.findFirst({
            where: {
                userId: userId,
                fetchType: "stream",
                broadcasterId: broadcasterId,
            },
            orderBy: {
                fetchedAt: "desc",
            },
        });
        // Assuming that there is only one fetch id on all stream
        if (fetchLog && fetchLog.fetchedAt > new Date(Date.now() - 5 * 60 * 1000)) {
            return prisma.stream.findFirst({
                where: {
                    fetchId: fetchLog.fetchId,
                },
                include: {
                    channel: true,
                    fetchLog: true,
                    tags: true,
                    videos: true,
                    categories: true,
                    titles: true,
                },
            });
        }
        const fetchId = uuidv4();
        const stream = await twitchService.getStreamByUserId(broadcasterId);
        if (stream === "offline") {
            return;
        }
        await prisma.fetchLog.create({
            data: {
                userId: userId,
                fetchedAt: new Date(),
                fetchId: fetchId,
                fetchType: "stream",
                broadcasterId: broadcasterId,
            },
        });

        await createStreamEntry(stream.stream, stream.tags, stream.category, stream.title, fetchId);

        return await prisma.stream.findFirst({
            where: {
                fetchId: fetchId,
            },
            include: {
                channel: true,
                fetchLog: true,
                tags: true,
                videos: true,
                categories: true,
                titles: true,
            },
        });
    } catch (error) {
        logger.error(`Error fetching stream: ${error}`);
        throw new Error("Error fetching stream");
    }
};

export const createStreamEntry = async (stream, tags, category, title, fetchId: string) => {
    try {
        await prisma.stream.upsert({
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
        if (tags.length > 0) {
            await tagService.addAllStreamTags(
                tags.map((tag) => ({ tagId: tag.name })),
                stream.id
            );
        }
        if (category) {
            await categoryService.addStreamCategory(stream.id, category.id);
        }
        if (title) {
            await titleService.addStreamTitle(stream.id, title.name);
        }
    } catch (error) {
        logger.error(`Error creating stream entry: ${error}`);
        throw new Error("Error creating stream entry");
    }
};

export const getChannelStream = async (broadcasterId: string, userId: string, accessToken: string) => {
    try {
        const fetchLog = await prisma.fetchLog.findFirst({
            where: {
                userId: userId,
                fetchType: "stream",
            },
            orderBy: {
                fetchedAt: "desc",
            },
        });
        if (fetchLog && fetchLog.fetchedAt > new Date(Date.now() - 5 * 60 * 1000)) {
            return prisma.stream.findMany({
                where: {
                    fetchId: fetchLog.fetchId,
                },
            });
        }
        const fetchId = uuidv4();
        const followedStreams = await twitchService.getAllFollowedStreams(userId, accessToken);
        await prisma.fetchLog.create({
            data: {
                userId: userId,
                fetchedAt: new Date(),
                fetchId: fetchId,
                fetchType: "followedStreams",
            },
        });
        for (let { stream, tags, category, title } of followedStreams) {
            await createStreamEntry(stream, tags, category, title, fetchId);
        }
        return followedStreams;
    } catch (error) {
        logger.error(`Error fetching followed streams: ${error}`);
        throw new Error("Error fetching followed streams");
    }
};

const ensureTagExists = async (tagId: string): Promise<void> => {
    await prisma.tag.upsert({
        where: { name: tagId },
        create: { name: tagId },
        update: {},
    });
};

const associateTagsWithStream = async (streamId: string, tags: string[]): Promise<void> => {
    for (const tag of tags) {
        await prisma.streamTag.upsert({
            where: { streamId_tagId: { streamId, tagId: tag } },
            create: {
                streamId,
                tagId: tag,
            },
            update: {},
        });
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

export const getUsersFollowedChannelsDb = async () => {
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

export const isFetchedFollowedStreamsIn = async (userId: string, dateTimeLimit: Date): Promise<boolean> => {
    try {
        const fetchLog = await prisma.fetchLog.findFirst({
            where: {
                userId: userId,
                fetchType: "followedStreams",
                fetchedAt: {
                    gte: dateTimeLimit,
                },
            },
        });

        return !!fetchLog;
    } catch (error) {
        logger.error("Error checking fetch log:", error);
        throw new Error("Error checking fetch log");
    }
};

// removed profile picture from it
export const getUserFollowedChannels = async (userId: string, accessToken: string) => {
    try {
        const oneDayAgo = new Date(Date.now() - 24 * 60 * 60 * 1000);
        if (await isFetchedFollowedStreamsIn(userId, oneDayAgo)) {
            return getUserFollowedChannelsDb(userId);
        }
        const followedChannels = await twitchService.getAllFollowedChannels(userId, accessToken);
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
                await prisma.userFollowedChannels.upsert({
                    where: {
                        broadcasterId_userId: {
                            broadcasterId: channel.broadcasterId,
                            userId: userId,
                        },
                    },
                    update: {
                        followed: true,
                        ...channel,
                    },
                    create: {
                        followed: true,
                        ...channel,
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

export const getChannelDetail = async (userId: string) => {
    return await twitchService.getUser(userId);
};

export const getChannelDetailDB = async (broadcasterId: string): Promise<Channel | null> => {
    let channel = await prisma.channel.findUnique({ where: { broadcasterId: broadcasterId } });
    if (!channel) {
        channel = await updateChannelDetail(broadcasterId);
    }
    return channel;
};

export const channelExists = async (broadcasterId: string): Promise<boolean> => {
    const channel = await prisma.channel.findUnique({
        where: {
            broadcasterId: broadcasterId,
        },
    });
    return !!channel;
};

export const getChannelDetailByNameDB = async (loginName: string) => {
    return prisma.channel.findUnique({
        where: { broadcasterLogin: loginName },
    });
};

export const getChannelBroadcasterIdByName = async (loginName: string) => {
    let channel;
    channel = await prisma.channel.findUnique({
        where: { broadcasterLogin: loginName },
    });
    if (!channel) {
        channel = await getChannelDetailByName(loginName);
    }
    return channel.broadcasterId;
};

export const getChannelDetailByName = async (username: string) => {
    const login = username.toLowerCase();
    return await twitchService.getUserByLogin(login);
};

export const getMultipleChannelDetailsDB = async (userIds: string[]) => {
    const channels = await prisma.channel.findMany({
        where: {
            broadcasterId: {
                in: userIds,
            },
        },
    });
    return channels;
};

export const updateChannelDetail = async (broadcaster_id: string) => {
    const channel = await twitchService.getUser(broadcaster_id);
    if (channel) {
        try {
            await prisma.channel.upsert({
                where: { broadcasterId: channel.broadcasterId },
                update: channel,
                create: channel,
            });
        } catch (error) {
            logger.error("Error updating channel details: %s", error);
        }
    }
    return channel;
};

// Transform twitch to db
export const fetchAndStoreChannelDetails = async (userIds: string[]) => {
    const users = await twitchService.getUsers(userIds);
    await storeUserDetails(users);
    return "Users fetched and stored successfully.";
};

export const storeUserDetails = async (channels: Channel[]) => {
    const upsertPromises = channels.map((channel) => {
        return prisma.channel.upsert({
            where: { broadcasterId: channel.broadcasterId },
            update: channel,
            create: channel,
        });
    });

    await Promise.all(upsertPromises);
};

export const isChannelFollowed = async (broadcasterId: string): Promise<boolean> => {
    const followedChannel = await prisma.userFollowedChannels.findFirst({
        where: {
            broadcasterId: broadcasterId,
        },
    });

    return !!followedChannel;
};

export const getChannelProfilePicture = async (broadcasterId: string): Promise<string | null> => {
    const channel = await prisma.channel.findUnique({
        where: {
            broadcasterId: broadcasterId,
        },
        select: {
            profilePicture: true,
        },
    });

    return channel?.profilePicture || null;
};

export const getChannelsProfilePicture = async (
    broadcasterIds: string[]
): Promise<Record<string, string | null>> => {
    const channels = await prisma.channel.findMany({
        where: {
            broadcasterId: {
                in: broadcasterIds,
            },
        },
        select: {
            broadcasterId: true,
            profilePicture: true,
        },
    });
    const profilePictures: Record<string, string | null> = {};
    for (const channel of channels) {
        profilePictures[channel.broadcasterId] = channel.profilePicture;
    }
    return profilePictures;
};

// From mongo: update all followed channels based on broadcasterId but now is SQL so its already linked to a channel
// export const updateUsers = async (userId: string) => {
//     const db = await getDbInstance();
//     const followedChannelsCollection = db.collection("followedChannels");
//     const userFollowedChannels = await followedChannelsCollection.findOne({ userId: userId });

//     if (!userFollowedChannels) {
//         throw new Error(`No document found for userId: ${userId}`);
//     }

//     const broadcasterIds = userFollowedChannels.channels.map((channel: FollowedChannel) => channel.broadcaster_id);
//     const existingUsers = await getMultipleChannelDetailsDB(broadcasterIds);
//     const existingUserIds = existingUsers.map((user) => user.id);
//     const newUserIds = broadcasterIds.filter((broadcasterId: string) => !existingUserIds.includes(broadcasterId));

//     if (newUserIds.length > 0) {
//         await fetchAndStoreChannelDetails(newUserIds);
//     }
//     return {
//         message: "Users update complete",
//         newUsers: `${newUserIds.length - existingUserIds.length} users added`,
//     };
// };

export async function getBroadcasterIds(): Promise<string[]> {
    const channels = await getUsersFollowedChannelsDb();
    return channels.map((channel) => channel.broadcasterId);
}
