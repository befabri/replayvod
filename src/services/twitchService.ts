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
import { StreamStatus } from "../models/streamMode";
import { EventSubMetaType } from "../integration/twitch/twitchSchema";

const logger = rootLogger.child({ domain: "twitch", service: "twitchService" });

class TwitchService {
    private api: TwitchAPI;

    constructor() {
        this.api = new TwitchAPI();
    }

    private async fetchData<T>(fetchFunction: () => Promise<T | null>, validator: (data: T) => boolean, transformer: (data: T) => any): Promise<any> {
        try {
            const data = await fetchFunction();
            if (!data) {
                throw new Error("Received null response from Twitch API");
            }
            if (!validator(data)) {
                throw new Error("Received invalid data from Twitch API: " + JSON.stringify(data));
            }
            return transformer(data);
        } catch (error) {
            throw new Error("Error during API call: %s", error);
        }
    }

    public async getUser(userId: string): Promise<Channel | null> {
        try {
            return await this.fetchData(
                () => this.api.getUser(userId),
                isValidUser,
                transformTwitchUser
            );
        } catch (error) {
            logger.error("Error fetching 'getUser': %s", error);
            return null;
        }
    }

    public async getUserByLogin(login: string): Promise<Channel | null> {
        try {
        return await this.fetchData(
            () => this.api.getUserByLogin(login.toLowerCase()),
            isValidUser,
            transformTwitchUser
            );
        } catch (error) {
            logger.error("Error fetching 'getUserByLogin': %s", error);
            return null;
        }
    }

    public async getUsers (userIds: string[]): Promise<Channel[] | null> {
        try {
        return await this.fetchData(
            () => this.api.getUsers(userIds),
            isValidUsers,
            (users) => users.map(transformTwitchUser)
            );
        } catch (error) {
            logger.error("Error fetching 'getUsers': %s", error);
            return null;
        }
    }

    public async getAllFollowedChannels(userId: string, accessToken: string, cursor?: string): Promise<UserFollowedChannels[] | null> {
        try {
        return await this.fetchData(
            () => this.api.getAllFollowedChannels(userId, accessToken, cursor),
            isValidFollowedChannel,
            (channels) => channels.map(channel => transformFollowedChannel(channel, userId))
            );
        } catch (error) {
            logger.error("Error fetching 'getAllFollowedChannels': %s", error);
            return null;
        }
    }

    public async getAllFollowedStreams(userId: string, accessToken: string, cursor?: string): Promise<{ stream: Stream; tags: Tag[]; category: Category; title: Omit<Title, "id"> }[] | null> {
        try {
        return await this.fetchData(
            () => this.api.getAllFollowedStreams(userId, accessToken, cursor),
            isValidStreams,
            (streams) => streams.map(transformStream)
            );
        } catch (error) {
            logger.error("Error fetching 'getAllFollowedStreams': %s", error);
            return null;
        }
    }

    public async getStreamByUserId(userId: string): Promise<{ stream: Stream; tags: Tag[]; category: Category; title: Omit<Title, "id"> } | StreamStatus.OFFLINE | null> {
        try {
            return await this.fetchData(
                () => this.api.getStreamByUserId(userId),
                isValidStream,
                transformStream
            );
        } catch (error) {
            logger.error("Error fetching 'getStreamByUserId': %s", error);
            return null;
        }
    }

    public async getGameDetail(gameId: string): Promise<Category | null> {
        try {
            return await this.fetchData(
                () => this.api.getGameDetail(gameId),
                isValidGame,
                transformCategory
            );
        } catch (error) {
            logger.error("Error fetching 'getGameDetail': %s", error);
            return null;
        }
    }

    public async getAllGames(cursor?: string): Promise<Category[] | null> {
        try {
            return await this.fetchData(
                () => this.api.getAllGames(cursor),
                isValidGames,
                (games) => games.map(transformCategory)
            );
        } catch (error) {
            logger.error("Error fetching 'getAllGames': %s", error);
            return null;
        }
    }

    public async createEventSub(type: string, version: string, condition: any, transport: any): Promise<Subscription[] | null> {
        try {
            return await this.fetchData(
                () => this.api.createEventSub(type, version, condition, transport),
                isValidEventSub,
                (eventSub) => eventSub.data.map(transformEventSub)
            );
        } catch (error) {
            logger.error("Error fetching 'createEventSub': %s", error);
            return null;
        }
    }

    public async getEventSub(): Promise<{ subscriptions: Subscription[]; meta: EventSubMetaType } | null> {
        try {
            return await this.fetchData(
                () => this.api.getEventSub(),
                isValidEventSub,
                (eventSub) => ({
                    subscriptions: eventSub.data.map(transformEventSub),
                    meta: transformEventSubMeta(eventSub)
                })
            );
        } catch (error) {
            logger.error("Error fetching 'getEventSub': %s", error);
            return null;
        }
    }

    public async deleteEventSub(id: string): Promise<any | null> {
        try {
            return await this.api.deleteEventSub(id);
        } catch (error) {
            logger.error("Error deleting 'eventSub': %s", error);
            return null;
        }
    }
}

export const twitchService = new TwitchService();
