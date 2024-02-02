import TwitchAPI from "../integration/twitch/twitchAPI";
import { logger as rootLogger } from "../app";
import { Category, Channel, Stream, Subscription, Tag, Title, UserFollowedChannels } from "@prisma/client";
import {
    isValidEventSubResponse,
    isValidFollowedChannel,
    isValidGame,
    isValidStream,
    isValidUser,
} from "../integration/twitch/validation";
import {
    transformCategory,
    transformEventSub,
    transformEventSubMeta,
    transformFollowedChannel,
    transformStream,
    transformTwitchUser,
} from "../integration/twitch/transformation";
import { EventSubMeta } from "../models/twitchModel";
import { StreamStatus } from "../models/streamMode";

const logger = rootLogger.child({ domain: "twitch", service: "twitchService" });
const twitchAPI = new TwitchAPI();

const getUser = async (userId: string): Promise<Channel | null> => {
    try {
        const fetchedUser = await twitchAPI.getUser(userId);
        if (!fetchedUser || !isValidUser(fetchedUser)) {
            logger.error("Received invalid user data from Twitch API: %s", fetchedUser);
            return null;
        }
        return transformTwitchUser(fetchedUser);
    } catch (error) {
        logger.error("Error getUser: %s", error);
        return null;
    }
};

const getUserByLogin = async (login: string): Promise<Channel | null> => {
    try {
        const fetchedUser = await twitchAPI.getUserByLogin(login.toLowerCase());
        if (!fetchedUser || !isValidUser(fetchedUser)) {
            logger.error("Received invalid user data from Twitch API: %s", fetchedUser);
            return null;
        }
        return transformTwitchUser(fetchedUser);
    } catch (error) {
        logger.error("Error getUserByLogin: %s", error);
        return null;
    }
};

const getUsers = async (userIds: string[]): Promise<Channel[] | null> => {
    try {
        const fetchedUsers = await twitchAPI.getUsers(userIds);
        if (!fetchedUsers || fetchedUsers.some((user) => !isValidUser(user))) {
            logger.error("Received invalid user data from Twitch API: %s", fetchedUsers);
            return null;
        }
        return fetchedUsers.map((user) => transformTwitchUser(user));
    } catch (error) {
        logger.error("Error fetching users: %s", error);
        return null;
    }
};

const getAllFollowedChannels = async (
    userId: string,
    accessToken: string,
    cursor?: string
): Promise<UserFollowedChannels[] | null> => {
    try {
        const followedChannels = await twitchAPI.getAllFollowedChannels(userId, accessToken, cursor);
        if (!followedChannels || followedChannels.some((channel) => !isValidFollowedChannel(channel))) {
            logger.error("Received invalid user data from Twitch API: %s", followedChannels);
            return null;
        }
        const transformedChannels = await Promise.all(
            followedChannels.map((channel) => transformFollowedChannel(channel, userId))
        );
        return transformedChannels;
    } catch (error) {
        logger.error("Error fetching followed channels: %s", error);
        return null;
    }
};

const getAllFollowedStreams = async (
    userId: string,
    accessToken: string,
    cursor?: string
): Promise<{ stream: Stream; tags: Tag[]; category: Category; title: Omit<Title, "id"> }[] | null> => {
    try {
        const followedStreams = await twitchAPI.getAllFollowedStreams(userId, accessToken, cursor);
        if (!followedStreams || followedStreams.some((stream) => !isValidStream(stream))) {
            logger.error("Received invalid stream data from Twitch API: %s", followedStreams);
            return null;
        }
        const transformationResults = await Promise.all(followedStreams.map((stream) => transformStream(stream)));
        return transformationResults;
    } catch (error) {
        logger.error("Error fetching followed streams: %s", error);
        return null;
    }
};

const getStreamByUserId = async (
    userId: string
): Promise<
    { stream: Stream; tags: Tag[]; category: Category; title: Omit<Title, "id"> } | StreamStatus.OFFLINE | null
> => {
    try {
        const fetchedStream = await twitchAPI.getStreamByUserId(userId);
        if (!fetchedStream) {
            return StreamStatus.OFFLINE;
        }
        if (!isValidStream(fetchedStream)) {
            logger.error("Received invalid stream data from Twitch API: %s", fetchedStream);
            return null;
        }
        return transformStream(fetchedStream);
    } catch (error) {
        logger.error("Error fetching stream by user ID: %s", error);
        return null;
    }
};

const getGameDetail = async (gameId: string): Promise<Category | null> => {
    try {
        const fetchedGame = await twitchAPI.getGameDetail(gameId);
        if (!fetchedGame || !isValidGame(fetchedGame)) {
            logger.error("Received invalid game detail data from Twitch API: %s", fetchedGame);
            return null;
        }
        return transformCategory(fetchedGame);
    } catch (error) {
        logger.error("Error fetching game detail: %s", error);
        return null;
    }
};

const getAllGames = async (cursor?: string): Promise<Category[] | null> => {
    try {
        const fetchedGames = await twitchAPI.getAllGames(cursor);
        if (!fetchedGames || fetchedGames.some((game) => !isValidGame(game))) {
            logger.error("Received invalid game data from Twitch API: %s", fetchedGames);
            return null;
        }
        return fetchedGames.map((game) => transformCategory(game));
    } catch (error) {
        logger.error("Error fetching all games: %s", error);
        return null;
    }
};

const createEventSub = async (
    type: string,
    version: string,
    condition: any,
    transport: any
): Promise<Subscription[] | null> => {
    try {
        const fetchedEventSub = await twitchAPI.createEventSub(type, version, condition, transport);
        if (!fetchedEventSub || !isValidEventSubResponse(fetchedEventSub)) {
            logger.error("Received invalid eventSub data from Twitch API: %s", fetchedEventSub);
            return null;
        }
        return fetchedEventSub.data.map((eventSub) => transformEventSub(eventSub));
    } catch (error) {
        logger.error("Error fetching create eventSub: %s", error);
        return null;
    }
};

const getEventSub = async (): Promise<{ subscriptions: Subscription[]; meta: EventSubMeta } | null> => {
    try {
        const fetchedEventSub = await twitchAPI.getEventSub();
        const validEventSub = isValidEventSubResponse(fetchedEventSub);
        if (!validEventSub) {
            logger.error("Received invalid eventSub data from Twitch API: %s", fetchedEventSub);
            return null;
        }
        const subscriptions = validEventSub.data.map((eventSub) => transformEventSub(eventSub));
        const meta = transformEventSubMeta(validEventSub);

        return {
            subscriptions,
            meta,
        };
    } catch (error) {
        logger.error("Error fetching eventSub: %s", error);
        return null;
    }
};

const deleteEventSub = async (id: string) => {
    try {
        const fetchedEventSub = await twitchAPI.deleteEventSub(id);
        return fetchedEventSub;
    } catch (error) {
        logger.error("Error deleteEventSub eventSub: %s", error);
        return null;
    }
};

export default {
    getUser,
    getUserByLogin,
    getUsers,
    getAllFollowedChannels,
    getAllFollowedStreams,
    getStreamByUserId,
    getGameDetail,
    getAllGames,
    createEventSub,
    getEventSub,
    deleteEventSub,
};
