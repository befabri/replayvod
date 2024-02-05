import TwitchAPI from "../integration/twitch/twitchAPI";
import { logger as rootLogger } from "../app";
import { Category, Channel, Stream, Subscription, Tag, Title, UserFollowedChannels } from "@prisma/client";
import {
    isValidEventSub,
    isValidFollowedChannel,
    isValidGame,
    isValidGames,
    isValidStream,
    isValidStreams,
    isValidUser,
    isValidUsers,
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
        if (!fetchedUser) {
            logger.error("Received null response 'getUser' from Twitch API");
            return null;
        }
        const validUser = isValidUser(fetchedUser);
        if (!validUser) {
            logger.error("Received invalid 'getUser' data from Twitch API: %s", JSON.stringify(fetchedUser));
            return null;
        }
        return transformTwitchUser(validUser);
    } catch (error) {
        logger.error("Error getUser: %s", error);
        return null;
    }
};

const getUserByLogin = async (login: string): Promise<Channel | null> => {
    try {
        const fetchedUser = await twitchAPI.getUserByLogin(login.toLowerCase());
        if (!fetchedUser) {
            logger.error("Received null response 'getUserByLogin' from Twitch API");
            return null;
        }
        const validUser = isValidUser(fetchedUser);
        if (!validUser) {
            logger.error(
                "Received invalid 'getUserByLogin' data from Twitch API: %s",
                JSON.stringify(fetchedUser)
            );
            return null;
        }
        return transformTwitchUser(validUser);
    } catch (error) {
        logger.error("Error getUserByLogin: %s", error);
        return null;
    }
};

const getUsers = async (userIds: string[]): Promise<Channel[] | null> => {
    try {
        const fetchedUsers = await twitchAPI.getUsers(userIds);
        if (!fetchedUsers) {
            logger.error("Received null response 'getUsers' from Twitch API");
            return null;
        }
        const validUsers = isValidUsers(fetchedUsers);
        if (!validUsers) {
            logger.error("Received invalid 'getUsers' data from Twitch API: %s", JSON.stringify(fetchedUsers));
            return null;
        }
        return validUsers.map(transformTwitchUser);
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
        const fetchedFollowedChannels = await twitchAPI.getAllFollowedChannels(userId, accessToken, cursor);
        if (!fetchedFollowedChannels) {
            logger.error("Received null response 'getAllFollowedChannels' from Twitch API");
            return null;
        }
        const validFollowedChannel = isValidFollowedChannel(fetchedFollowedChannels);
        if (!validFollowedChannel) {
            logger.error(
                "Received invalid 'getAllFollowedChannels' data from Twitch API: %s",
                JSON.stringify(fetchedFollowedChannels)
            );
            return null;
        }
        return validFollowedChannel.map((channel) => transformFollowedChannel(channel, userId));
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
        const fetchedFollowedStreams = await twitchAPI.getAllFollowedStreams(userId, accessToken, cursor);
        if (!fetchedFollowedStreams) {
            logger.error("Received null response 'getAllFollowedStreams' from Twitch API");
            return null;
        }
        const validStreams = isValidStreams(fetchedFollowedStreams);
        if (!validStreams) {
            logger.error(
                "Received invalid 'getAllFollowedStreams' data from Twitch API: %s",
                JSON.stringify(fetchedFollowedStreams)
            );
            return null;
        }
        return validStreams.map(transformStream);
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
        const validStream = isValidStream(fetchedStream);
        if (!validStream) {
            logger.error(
                "Received invalid 'getStreamByUserId' data from Twitch API: %s",
                JSON.stringify(fetchedStream)
            );
            return null;
        }
        return transformStream(validStream);
    } catch (error) {
        logger.error("Error fetching stream by user ID: %s", error);
        return null;
    }
};

const getGameDetail = async (gameId: string): Promise<Category | null> => {
    try {
        if(!gameId){
            return null
        }
        const fetchedGame = await twitchAPI.getGameDetail(gameId);
        if (!fetchedGame) {
            logger.error("Received null response 'getGameDetail' from Twitch API");
            return null;
        }
        const validEventSub = isValidGame(fetchedGame);
        if (!validEventSub) {
            logger.error("Received invalid 'getGameDetail' data from Twitch API: %s", JSON.stringify(fetchedGame));
            return null;
        }
        return transformCategory(validEventSub);
    } catch (error) {
        logger.error("Error fetching game detail: %s", error);
        return null;
    }
};

const getAllGames = async (cursor?: string): Promise<Category[] | null> => {
    try {
        const fetchedGames = await twitchAPI.getAllGames(cursor);
        if (!fetchedGames) {
            logger.error("Received null response 'getAllGames' from Twitch API");
            return null;
        }
        const validUsers = isValidGames(fetchedGames);
        if (!validUsers) {
            logger.error("Received invalid 'getAllGames' data from Twitch API: %s", JSON.stringify(fetchedGames));
            return null;
        }
        return validUsers.map(transformCategory);
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
        if (!fetchedEventSub) {
            logger.error("Received null response 'createEventSub' from Twitch API");
            return null;
        }
        const validEventSub = isValidEventSub(fetchedEventSub);
        if (!validEventSub) {
            logger.error(
                "Received invalid 'createEventSub' data from Twitch API: %s",
                JSON.stringify(fetchedEventSub)
            );
            return null;
        }
        return validEventSub.data.map((eventSub) => transformEventSub(eventSub));
    } catch (error) {
        logger.error("Error fetching create eventSub: %s", error);
        return null;
    }
};

const getEventSub = async (): Promise<{ subscriptions: Subscription[]; meta: EventSubMeta } | null> => {
    try {
        const fetchedEventSub = await twitchAPI.getEventSub();
        const validEventSub = isValidEventSub(fetchedEventSub);
        if (!validEventSub) {
            logger.error(
                "Received invalid 'getEventSub' data from Twitch API: %s",
                JSON.stringify(fetchedEventSub)
            );
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
        logger.error("Error 'deleteEventSub' eventSub: %s", error);
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
