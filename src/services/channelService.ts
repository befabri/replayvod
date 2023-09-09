import TwitchAPI from "../utils/twitchAPI";
import { v4 as uuidv4 } from "uuid";
import { User, FollowedChannel, FollowedStream } from "../models/twitchModel";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
import { Channel, FetchLog, Prisma } from "@prisma/client";
const logger = rootLogger.child({ service: "channelService" });

const twitchAPI = new TwitchAPI();

const mapTwitchStreamToPrismaStream = (
    twitchStream: FollowedStream,
    fetchId: string
): Prisma.StreamCreateInput => {
    return {
        id: twitchStream.id,
        fetchId: fetchId,
        fetchedAt: new Date(),
        categoryId: twitchStream.game_id,
        categoryName: twitchStream.game_name,
        isMature: false,
        language: twitchStream.language,
        startedAt: new Date(twitchStream.started_at),
        thumbnailUrl: twitchStream.thumbnail_url,
        title: twitchStream.title,
        type: twitchStream.type,
        broadcasterId: twitchStream.user_id,
        broadcasterLogin: twitchStream.user_login,
        broadcasterName: twitchStream.user_name,
        viewerCount: twitchStream.viewer_count,
    };
};

export const getUserFollowedStreams = async (userId: string, accessToken: string) => {
    try {
        const fetchLog = await prisma.fetchLog.findFirst({
            where: {
                userId: userId,
                type: "followedStreams",
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
        const followedStreams = await twitchAPI.getAllFollowedStreams(userId, accessToken);
        const streamUserIds = followedStreams.map((stream: FollowedStream) => stream.user_id);
        const users = await twitchAPI.getUsers(streamUserIds);
        await storeUserDetails(users);
        for (const stream of followedStreams) {
            const prismaStreamData = mapTwitchStreamToPrismaStream(stream, fetchId);
            await prisma.stream.upsert({
                where: { id: stream.id },
                create: prismaStreamData,
                update: {
                    ...prismaStreamData,
                    tags: undefined,
                    videos: undefined,
                },
            });

            // Ensure tags exist
            for (const tag of stream.tag_ids) {
                await ensureTagExists(tag);
            }

            // Associate tags with the stream
            await associateTagsWithStream(stream.id, stream.tag_ids);
        }
        await prisma.fetchLog.create({
            data: {
                userId: userId,
                fetchedAt: new Date(),
                fetchId: fetchId,
                type: "followedStreams",
            },
        });

        return followedStreams;
    } catch (error) {
        console.error("Error fetching followed streams:", error);
        throw new Error("Error fetching followed streams");
    }
};
//This helper function checks if a tag exists and if not, creates it.
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
        });
        return followedChannelsRelations.map((relation) => relation.channel);
    } catch (error) {
        console.error("Error getting user followed channels from Db:", error);
        throw new Error("Error getting user followed channels from Db");
    }
};

export const getUsersFollowedChannelsDb = async () => {
    try {
        const followedChannelsRelations = await prisma.userFollowedChannels.findMany({
            include: {
                channel: true,
            },
        });
        return followedChannelsRelations.map((relation) => relation.channel);
    } catch (error) {
        console.error("Error getting followed channels from Db:", error);
        throw new Error("Error getting followed channels from Db");
    }
};

export const isFetchedFollowedStreamsIn = async (userId: string, dateTimeLimit: Date): Promise<boolean> => {
    try {
        const fetchLog = await prisma.fetchLog.findFirst({
            where: {
                userId: userId,
                type: "followedStreams",
                fetchedAt: {
                    gte: dateTimeLimit,
                },
            },
        });

        return !!fetchLog;
    } catch (error) {
        console.error("Error checking fetch log:", error);
        throw new Error("Error checking fetch log");
    }
};

export const getUserFollowedChannels = async (userId: string, accessToken: string) => {
    try {
        const oneDayAgo = new Date(Date.now() - 24 * 60 * 60 * 1000);
        if (await isFetchedFollowedStreamsIn(userId, oneDayAgo)) {
            return getUserFollowedChannelsDb(userId);
        }
        const followedChannels = await twitchAPI.getAllFollowedChannels(userId, accessToken);
        const channelsUserIds = followedChannels.map((channel: FollowedChannel) => channel.broadcaster_id);
        const users = await twitchAPI.getUsers(channelsUserIds);
        await storeUserDetails(users);
        const profilePictures = await getUserProfilePicture(channelsUserIds);
        const followedChannelsWithProfilePictures = followedChannels.map((channel) => ({
            ...channel,
            profile_picture: profilePictures[channel.broadcaster_id],
        }));

        await followedChannelsCollection.updateOne(
            { userId },
            {
                $set: {
                    channels: followedChannelsWithProfilePictures,
                    fetchedAt: new Date(),
                    userId,
                },
            },
            { upsert: true }
        );
        return followedChannelsWithProfilePictures;
    } catch (error) {
        console.error("Error fetching followed channels from Twitch API:", error);
        throw new Error("Error fetching followed channels from Twitch API");
    }
};

export const getChannelDetail = async (userId: string) => {
    return await twitchAPI.getUser(userId);
};

export const getChannelDetailDB = async (broadcasterId: string): Promise<Channel | null> => {
    let channel = await prisma.channel.findUnique({ where: { broadcasterId: broadcasterId } });
    if (!channel) {
        channel = await updateUserDetail(broadcasterId);
    }
    return channel;
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
    return await twitchAPI.getUserByLogin(login);
};

export const getMultipleChannelDetailsDB = async (userIds: string[]) => {
    const db = await getDbInstance();
    const userCollection = db.collection("users");
    const users = [];
    for (const id of userIds) {
        if (typeof id === "string") {
            const user = await userCollection.findOne({ id });
            if (user) {
                users.push(user);
            }
        }
    }
    return users;
};

export const updateUserDetail = async (userId: string) => {
    const user = await twitchAPI.getUser(userId);
    if (user) {
        const db = await getDbInstance();
        const userCollection = db.collection("users");
        await userCollection.updateOne({ id: userId }, { $set: user }, { upsert: true });
    }
    return user;
};

export const fetchAndStoreChannelDetails = async (userIds: string[]) => {
    const users = await twitchAPI.getUsers(userIds);
    await storeUserDetails(users);
    return "Users fetched and stored successfully.";
};

export const storeUserDetails = async (users: any[]) => {
    const db = await getDbInstance();
    const userCollection = db.collection("users");
    const bulkOps = users.map((user) => ({
        updateOne: {
            filter: { id: user.id },
            update: { $set: user },
            upsert: true,
        },
    }));
    await userCollection.bulkWrite(bulkOps);
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

export const updateUsers = async (userId: string) => {
    const db = await getDbInstance();
    const followedChannelsCollection = db.collection("followedChannels");
    const userFollowedChannels = await followedChannelsCollection.findOne({ userId: userId });

    if (!userFollowedChannels) {
        throw new Error(`No document found for userId: ${userId}`);
    }

    const broadcasterIds = userFollowedChannels.channels.map((channel: FollowedChannel) => channel.broadcaster_id);
    const existingUsers = await getMultipleUserDetailsDB(broadcasterIds);
    const existingUserIds = existingUsers.map((user) => user.id);
    const newUserIds = broadcasterIds.filter((broadcasterId: string) => !existingUserIds.includes(broadcasterId));

    if (newUserIds.length > 0) {
        await fetchAndStoreUserDetails(newUserIds);
    }
    return {
        message: "Users update complete",
        newUsers: `${newUserIds.length - existingUserIds.length} users added`,
    };
};

export default {
    getUserFollowedStreams,
    getUserFollowedChannelsDb,
    getUserFollowedChannels,
    getChannelDetail,
    getChannelDetailDB,
    getChannelDetailByName,
    getMultipleChannelDetailsDB,
    updateUserDetail,
    fetchAndStoreChannelDetails,
    storeUserDetails,
    getChannelProfilePicture,
    updateUsers,
    getChannelDetailByNameDB,
    getChannelBroadcasterIdByName,
};
