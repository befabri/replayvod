import TwitchAPI from "../utils/twitchAPI";
import { logger as rootLogger } from "../app";
import { Category, Channel, Stream, Subscription, UserFollowedChannels } from "@prisma/client";
import {
    isValidEventSubResponse,
    isValidFollowedChannel,
    isValidFollowedStream,
    isValidGame,
    isValidStream,
    isValidUser,
} from "../utils/validation";
import {
    transformCategory,
    transformEventSub,
    transformEventSubMeta,
    transformFollowedChannel,
    transformFollowedStream,
    transformStream,
    transformTwitchUser,
} from "../utils/transformation";
import { EventSubMeta } from "../models/twitchModel";

const logger = rootLogger.child({ service: "twitchService" });
const twitchAPI = new TwitchAPI();

export const getUser = async (userId: string): Promise<Channel | null> => {
    try {
        const fetchedUser = await twitchAPI.getUser(userId);
        if (!fetchedUser || !isValidUser(fetchedUser)) {
            logger.error("Received invalid user data from Twitch API:", fetchedUser);
            return null;
        }
        return transformTwitchUser(fetchedUser);
    } catch (error) {
        throw error;
    }
};

export const getUserByLogin = async (login: string): Promise<Channel | null> => {
    try {
        const fetchedUser = await twitchAPI.getUserByLogin(login);
        if (!fetchedUser || !isValidUser(fetchedUser)) {
            logger.error("Received invalid user data from Twitch API:", fetchedUser);
            return null;
        }
        return transformTwitchUser(fetchedUser);
    } catch (error) {
        throw error;
    }
};

export const getUsers = async (userIds: string[]): Promise<Channel[] | null> => {
    try {
        const fetchedUsers = await twitchAPI.getUsers(userIds);
        if (!fetchedUsers || fetchedUsers.some((user) => !isValidUser(user))) {
            logger.error("Received invalid user data from Twitch API:", fetchedUsers);
            return null;
        }
        return fetchedUsers.map((user) => transformTwitchUser(user));
    } catch (error) {
        throw error;
    }
};

//
export const getAllFollowedChannels = async (
    userId: string,
    accessToken: string,
    cursor?: string
): Promise<UserFollowedChannels[] | null> => {
    try {
        const followedChannels = await twitchAPI.getAllFollowedChannels(userId, accessToken, cursor);
        if (!followedChannels || followedChannels.some((channel) => !isValidFollowedChannel(channel))) {
            logger.error("Received invalid user data from Twitch API:", followedChannels);
            return null;
        }
        const transformedChannels = await Promise.all(
            followedChannels.map((channel) => transformFollowedChannel(channel, userId))
        );
        return transformedChannels;
    } catch (error) {
        logger.error("Error fetching followed channels:", error);
        throw error;
    }
};

export const getAllFollowedStreams = async (
    userId: string,
    accessToken: string,
    cursor?: string
): Promise<Stream[] | null> => {
    try {
        const followedStreams = await twitchAPI.getAllFollowedStreams(userId, accessToken, cursor);
        if (!followedStreams || followedStreams.some((stream) => !isValidFollowedStream(stream))) {
            logger.error("Received invalid stream data from Twitch API:", followedStreams);
            return null;
        }
        const transformedStreams = await Promise.all(
            followedStreams.map((stream) => transformFollowedStream(stream))
        );
        return transformedStreams;
    } catch (error) {
        logger.error("Error fetching followed streams:", error);
        throw error;
    }
};

export const getStreamByUserId = async (userId: string): Promise<Stream | null> => {
    try {
        const fetchedStream = await twitchAPI.getStreamByUserId(userId);
        if (!fetchedStream || !isValidStream(fetchedStream)) {
            logger.error("Received invalid stream data from Twitch API:", fetchedStream);
            return null;
        }
        return transformStream(fetchedStream);
    } catch (error) {
        logger.error("Error fetching stream by user ID:", error);
        throw error;
    }
};

export const getAllGames = async (cursor?: string): Promise<Category[] | null> => {
    try {
        const fetchedGames = await twitchAPI.getAllGames(cursor);
        if (!fetchedGames || fetchedGames.some((game) => !isValidGame(game))) {
            logger.error("Received invalid game data from Twitch API:", fetchedGames);
            return null;
        }
        return fetchedGames.map((game) => transformCategory(game));
    } catch (error) {
        logger.error("Error fetching all games:", error);
        throw error;
    }
};

export const createEventSub = async (
    type: string,
    version: string,
    condition: any,
    transport: any
): Promise<Subscription[] | null> => {
    try {
        const fetchedEventSub = await twitchAPI.createEventSub(type, version, condition, transport);
        if (!fetchedEventSub || !isValidEventSubResponse(fetchedEventSub)) {
            logger.error("Received invalid eventSub data from Twitch API:", fetchedEventSub);
            return null;
        }
        return fetchedEventSub.data.map((eventSub) => transformEventSub(eventSub));
    } catch (error) {
        logger.error("Error fetching create eventSub:", error);
        throw error;
    }
};

export const getEventSub = async (): Promise<{ subscriptions: Subscription[]; meta: EventSubMeta } | null> => {
    try {
        const fetchedEventSub = await twitchAPI.getEventSub();
        if (!fetchedEventSub || !isValidEventSubResponse(fetchedEventSub)) {
            logger.error("Received invalid eventSub data from Twitch API:", fetchedEventSub);
            return null;
        }

        const subscriptions = fetchedEventSub.data.map((eventSub) => transformEventSub(eventSub));
        const meta = transformEventSubMeta(fetchedEventSub);

        return {
            subscriptions,
            meta,
        };
    } catch (error) {
        throw error;
    }
};

export const deleteEventSub = async (id: string) => {
    try {
        const fetchedEventSub = await twitchAPI.deleteEventSub(id);
        return fetchedEventSub;
    } catch (error) {
        throw error;
    }
};

export default {
    getUser,
    getUserByLogin,
    getUsers,
    getAllFollowedChannels,
    getAllFollowedStreams,
    getStreamByUserId,
    getAllGames,
    createEventSub,
    getEventSub,
    deleteEventSub,
};
