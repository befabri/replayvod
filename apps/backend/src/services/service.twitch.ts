import TwitchAPI from "../integration/twitch/twitch.api";
import { env, logger as rootLogger } from "../app";
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
} from "../integration/twitch/twitch.validation";
import {
    transformCategory,
    transformEventSub,
    transformEventSubMeta,
    transformFollowedChannel,
    transformStream,
    transformTwitchUser,
} from "../integration/twitch/twitch.transformation";
import { EventSubMetaType } from "../integration/twitch/twitch.schema";
import { StreamStatus } from "../models/model.twitch";
import { PrismaClient } from "@prisma/client/extension";
import axios from "axios";

const logger = rootLogger.child({ domain: "service", service: "twitch" });

export class TwitchService {
    private twitchClientId: string;
    private twitchSecret: string;
    private api: TwitchAPI;

    constructor(private db: PrismaClient) {
        if (!env.twitchClientId || !env.twitchSecret) {
            throw new Error("Missing .env: env.twitchClientId and/or env.twitchSecret");
        }
        this.twitchClientId = env.twitchClientId;
        this.twitchSecret = env.twitchSecret;
        this.api = new TwitchAPI(this.getAppAccessToken.bind(this));
    }

    getAppAccessToken = async () => {
        const latestToken = await this.db.appAccessToken.findFirst({
            orderBy: {
                expiresAt: "desc",
            },
        });
        if (latestToken && new Date(latestToken.expiresAt) > new Date(Date.now() + 300000)) {
            // 5 min
            return latestToken.accessToken;
        }

        try {
            const token = await this.fetchAppAccessToken();
            await this.saveAppAccessToken(token.access_token, token.expires_in);
            return token.access_token;
        } catch (error) {
            logger.error("Error getting app access token: %s", error);
            throw error;
        }
    };

    fetchAppAccessToken = async () => {
        logger.info("Fetching access token...");
        try {
            const response = await axios.post("https://id.twitch.tv/oauth2/token", null, {
                params: {
                    client_id: this.twitchClientId,
                    client_secret: this.twitchSecret,
                    grant_type: "client_credentials",
                },
            });
            return response.data;
        } catch (error) {
            logger.error("Error fetching app access token: %s", error);
            throw error;
        }
    };

    saveAppAccessToken = async (accessToken: string, expiresIn: number) => {
        logger.info("Saving access token...");
        try {
            const tokenLifetime = expiresIn * 1000;
            const currentTimestamp = new Date();
            const expiresAt = new Date(currentTimestamp.getTime() + tokenLifetime);
            await this.db.appAccessToken.create({
                data: {
                    accessToken: accessToken,
                    expiresAt: expiresAt,
                },
            });
        } catch (error) {
            logger.error("Error saving app access token: %s", error);
            throw error;
        }
    };

    // Todo used it
    cleanupExpiredTokens = async () => {
        const currentTimestamp = new Date();
        await this.db.appAccessToken.deleteMany({
            where: {
                expiresAt: {
                    lte: currentTimestamp,
                },
            },
        });
    };

    private async fetchData<T>(
        fetchFunction: () => Promise<T | null>,
        validator: (data: T) => boolean,
        transformer: (data: T) => any
    ): Promise<any> {
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
            throw new Error(`Error during API call: ${error}`);
        }
    }

    public async getUser(userId: string): Promise<Channel | null> {
        try {
            return await this.fetchData(() => this.api.getUser(userId), isValidUser, transformTwitchUser);
        } catch (error) {
            logger.error(`Error fetching getUser: ${error}`);
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
            logger.error(`Error fetching getUserByLogin: ${error}`);
            return null;
        }
    }

    public async getUsers(userIds: string[]): Promise<Channel[] | null> {
        try {
            return await this.fetchData(
                () => this.api.getUsers(userIds),
                isValidUsers,
                (users) => users.map(transformTwitchUser)
            );
        } catch (error) {
            logger.error(`Error fetching getUsers: ${error}`);
            return null;
        }
    }

    public async getAllFollowedChannels(
        userId: string,
        accessToken: string,
        cursor?: string
    ): Promise<UserFollowedChannels[] | null> {
        try {
            return await this.fetchData(
                () => this.api.getAllFollowedChannels(userId, accessToken, cursor),
                isValidFollowedChannel,
                (channels) => channels.map((channel) => transformFollowedChannel(channel, userId))
            );
        } catch (error) {
            logger.error(`Error fetching getAllFollowedChannels: ${error}`);
            return null;
        }
    }

    public async getAllFollowedStreams(
        userId: string,
        accessToken: string,
        cursor?: string
    ): Promise<{ stream: Stream; tags: Tag[]; category: Category; title: Omit<Title, "id"> }[] | null> {
        try {
            return await this.fetchData(
                () => this.api.getAllFollowedStreams(userId, accessToken, cursor),
                isValidStreams,
                (streams) => streams.map(transformStream)
            );
        } catch (error) {
            logger.error(`Error fetching getAllFollowedStreams: ${error}`);
            return null;
        }
    }

    public async getStreamByUserId(
        userId: string
    ): Promise<
        { stream: Stream; tags: Tag[]; category: Category; title: Omit<Title, "id"> } | StreamStatus.OFFLINE | null
    > {
        try {
            return await this.fetchData(() => this.api.getStreamByUserId(userId), isValidStream, transformStream);
        } catch (error) {
            logger.error(`Error fetching getStreamByUserId: ${error}`);
            return null;
        }
    }

    public async getGameDetail(gameId: string): Promise<Category | null> {
        try {
            return await this.fetchData(() => this.api.getGameDetail(gameId), isValidGame, transformCategory);
        } catch (error) {
            logger.error(`Error fetching getGameDetail: ${error}`);
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
            logger.error(`Error fetching getAllGames: ${error}`);
            return null;
        }
    }

    public async createEventSub(
        type: string,
        version: string,
        condition: any,
        transport: any
    ): Promise<Subscription[] | null> {
        try {
            return await this.fetchData(
                () => this.api.createEventSub(type, version, condition, transport),
                isValidEventSub,
                (eventSub) => eventSub.data.map(transformEventSub)
            );
        } catch (error) {
            logger.error(`Error fetching createEventSub: ${error}`);
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
                    meta: transformEventSubMeta(eventSub),
                })
            );
        } catch (error) {
            logger.error(`Error fetching getEventSub: ${error}`);
            return null;
        }
    }

    public async deleteEventSub(id: string): Promise<any | null> {
        try {
            return await this.api.deleteEventSub(id);
        } catch (error) {
            logger.error(`Error fetching deleteEventSub: ${error}`);
            return null;
        }
    }
}
