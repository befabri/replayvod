import axios from "axios";
import { getAppAccessToken } from "./twitchUtils";
import { chunkArray } from "../../utils/utils";
import { Stream, User, FollowedChannel, EventSubResponse } from "../../models/twitchModel";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "twitch", service: "twitchApi" });

import dotenv from "dotenv";
dotenv.config();
const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;

class TwitchAPI {
    async getUser(userId: string): Promise<User | null> {
        try {
            const accessToken = await getAppAccessToken();
            const response = await axios.get(`https://api.twitch.tv/helix/users?id=${userId}`, {
                headers: {
                    Authorization: "Bearer " + accessToken,
                    "Client-ID": TWITCH_CLIENT_ID,
                },
            });

            return response.data.data[0] || null;
        } catch (error) {
            logger.error("Error fetching user details from Twitch API:", error);
            throw new Error("Failed to fetch user details from Twitch API");
        }
    }

    async getUserByLogin(login: string): Promise<User | null> {
        try {
            const accessToken = await getAppAccessToken();
            const response = await axios.get(`https://api.twitch.tv/helix/users?login=${login}`, {
                headers: {
                    Authorization: "Bearer " + accessToken,
                    "Client-ID": TWITCH_CLIENT_ID,
                },
            });

            return response.data.data[0] || null;
        } catch (error) {
            logger.error("Error fetching user details from Twitch API:", error);
            throw new Error("Failed to fetch user details from Twitch API");
        }
    }

    async getUsers(userIds: string[]): Promise<User[]> {
        try {
            const accessToken = await getAppAccessToken();
            const userIdChunks = chunkArray(userIds, 100);
            const responses = await Promise.all(
                userIdChunks.map(async (chunk) => {
                    const params = chunk.map((id) => `id=${id}`).join("&");
                    return axios.get(`https://api.twitch.tv/helix/users?${params}`, {
                        headers: {
                            Authorization: "Bearer " + accessToken,
                            "Client-ID": TWITCH_CLIENT_ID,
                        },
                    });
                })
            );
            return responses.flatMap((response) => response.data.data);
        } catch (error) {
            logger.error("Error fetching users details from Twitch API:", error);
            throw new Error("Failed to fetch users details from Twitch API");
        }
    }

    async getAllFollowedChannels(
        userId: string,
        accessToken: string,
        cursor?: string
    ): Promise<FollowedChannel[]> {
        const response = await axios({
            method: "get",
            url: `https://api.twitch.tv/helix/channels/followed`,
            params: {
                user_id: userId,
                after: cursor,
                first: 100,
            },
            headers: {
                Authorization: `Bearer ${accessToken}`,
                "Client-Id": TWITCH_CLIENT_ID,
            },
        });

        const { data, pagination } = response.data;
        if (pagination && pagination.cursor) {
            const nextPageData = await this.getAllFollowedChannels(userId, accessToken, pagination.cursor);
            return data.concat(nextPageData);
        } else {
            return data;
        }
    }

    async getAllFollowedStreams(userId: string, accessToken: string, cursor?: string): Promise<Stream[]> {
        const response = await axios({
            method: "get",
            url: `https://api.twitch.tv/helix/streams/followed`,
            params: {
                user_id: userId,
                after: cursor,
                first: 100,
            },
            headers: {
                Authorization: `Bearer ${accessToken}`,
                "Client-Id": TWITCH_CLIENT_ID,
            },
        });

        const { data, pagination } = response.data;
        if (pagination && pagination.cursor) {
            const nextPageData = await this.getAllFollowedStreams(userId, accessToken, pagination.cursor);
            return data.concat(nextPageData);
        } else {
            return data;
        }
    }

    async getStreamByUserId(userId: string): Promise<Stream> {
        try {
            const accessToken = await getAppAccessToken();
            const response = await axios.get(`https://api.twitch.tv/helix/streams?user_id=${userId}`, {
                headers: {
                    Authorization: `Bearer ${accessToken}`,
                    "Client-ID": TWITCH_CLIENT_ID,
                },
            });
            return response.data.data[0] || null;
        } catch (error) {
            logger.error("Error fetching stream details from Twitch API:", error);
            throw new Error("Failed to fetch stream details from Twitch API");
        }
    }

    async getAllGames(cursor?: string): Promise<any[]> {
        const accessToken = await getAppAccessToken();
        const response = await axios.get("https://api.twitch.tv/helix/games/top", {
            headers: {
                Authorization: `Bearer ${accessToken}`,
                "Client-ID": TWITCH_CLIENT_ID,
            },
            params: {
                first: 100,
                after: cursor,
            },
        });

        const { data, pagination } = response.data;
        if (pagination && pagination.cursor) {
            await new Promise((resolve) => setTimeout(resolve, 3000));
            const nextPageData = await this.getAllGames(pagination.cursor);
            return data.concat(nextPageData);
        } else {
            return data;
        }
    }

    async createEventSub(
        type: string,
        version: string,
        condition: any,
        transport: any
    ): Promise<EventSubResponse | null> {
        const accessToken = await getAppAccessToken();
        try {
            const response = await axios.post(
                "https://api.twitch.tv/helix/eventsub/subscriptions",
                {
                    type,
                    version,
                    condition,
                    transport,
                },
                {
                    headers: {
                        Authorization: `Bearer ${accessToken}`,
                        "Client-ID": TWITCH_CLIENT_ID,
                        "Content-Type": "application/json",
                    },
                }
            );
            return response.data;
        } catch (error) {
            if (error.response) {
                logger.error(error.response.data.message);
                throw error;
            } else {
                throw error;
            }
        }
    }

    async deleteEventSub(id: string) {
        const accessToken = await getAppAccessToken();
        try {
            const response = await axios.delete(`https://api.twitch.tv/helix/eventsub/subscriptions?id=${id}`, {
                headers: {
                    Authorization: `Bearer ${accessToken}`,
                    "Client-ID": TWITCH_CLIENT_ID,
                },
            });

            if (response.status === 204) {
                return "Subscription successfully deleted.";
            } else {
                return response.data;
            }
        } catch (error) {
            if (error.response) {
                switch (error.response.status) {
                    case 400:
                        throw new Error("The id query parameter is required.");
                    case 401:
                        throw new Error("Authorization error. Please check the access token and Client-ID.");
                    case 404:
                        throw new Error("The subscription was not found.");
                    default:
                        throw new Error("An unknown error occurred.");
                }
            } else {
                throw error;
            }
        }
    }

    async getEventSub(): Promise<EventSubResponse> {
        const accessToken = await getAppAccessToken();
        try {
            const response = await axios.get("https://api.twitch.tv/helix/eventsub/subscriptions", {
                headers: {
                    Authorization: `Bearer ${accessToken}`,
                    "Client-ID": TWITCH_CLIENT_ID,
                },
            });

            return response.data;
        } catch (error) {
            throw error;
        }
    }
}

export default TwitchAPI;
