import axios from "axios";
import { chunkArray } from "../../utils/utils";
import { env, logger as rootLogger } from "../../app";
import { getAppAccessToken } from "./twitchAccessToken";
import { EventSubType, FollowedChannelArrayType, GameArrayType, GameType, StreamArrayType, StreamType, UserArrayType, UserType } from "./twitchSchema";
const logger = rootLogger.child({ domain: "twitch", service: "twitchApi" });

function handleAxiosError(error: unknown) {
    if (axios.isAxiosError(error)) {
        if (error.response) {
            logger.error(`Server responded with error: ${error.response.status} - ${error.response.data.message}`);
        } else if (error.request) {
            logger.error("The request was made but no response was received");
        } else {
            logger.error(`Error in setting up the request: ${error.message}`);
        }
    } else if (error instanceof Error) {
        logger.error(`Non-Axios error: ${error.message}`);
    } else {
        logger.error("An unknown error occurred");
    }
}

class TwitchAPI {
    async getUser(userId: string): Promise<UserType | null> {
        try {
            const accessToken = await getAppAccessToken();
            const response = await axios.get(`https://api.twitch.tv/helix/users?id=${userId}`, {
                headers: {
                    Authorization: "Bearer " + accessToken,
                    "Client-ID": env.twitchClientId,
                },
            });

            return response.data.data[0] || null;
        } catch (error) {
            logger.error("Error fetching user details from Twitch API: %s", error);
            throw new Error("Failed to fetch user details from Twitch API");
        }
    }

    async getUserByLogin(login: string): Promise<UserType | null> {
        try {
            const accessToken = await getAppAccessToken();
            const response = await axios.get(`https://api.twitch.tv/helix/users?login=${login}`, {
                headers: {
                    Authorization: "Bearer " + accessToken,
                    "Client-ID": env.twitchClientId,
                },
            });

            return response.data.data[0] || null;
        } catch (error) {
            logger.error("Error fetching user details from Twitch API: %s", error);
            throw new Error("Failed to fetch user details from Twitch API");
        }
    }

    async getUsers(userIds: string[]): Promise<UserArrayType> {
        try {
            const accessToken = await getAppAccessToken();
            const userIdChunks = chunkArray(userIds, 100);
            const responses = await Promise.all(
                userIdChunks.map(async (chunk) => {
                    const params = chunk.map((id) => `id=${id}`).join("&");
                    return axios.get(`https://api.twitch.tv/helix/users?${params}`, {
                        headers: {
                            Authorization: "Bearer " + accessToken,
                            "Client-ID": env.twitchClientId,
                        },
                    });
                })
            );
            return responses.flatMap((response) => response.data.data);
        } catch (error) {
            handleAxiosError(error);
            throw new Error("Failed to fetch users details from Twitch API");
        }
    }

    async getAllFollowedChannels(
        userId: string,
        accessToken: string,
        cursor?: string
    ): Promise<FollowedChannelArrayType> {
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
                "Client-Id": env.twitchClientId,
            },
        });

        const { data, pagination } = response.data;
        if (pagination && pagination.cursor) {
            await new Promise((resolve) => setTimeout(resolve, 3000));
            const nextPageData = await this.getAllFollowedChannels(userId, accessToken, pagination.cursor);
            return data.concat(nextPageData);
        } else {
            return data;
        }
    }

    async getAllFollowedStreams(userId: string, accessToken: string, cursor?: string): Promise<StreamArrayType> {
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
                "Client-Id": env.twitchClientId,
            },
        });

        const { data, pagination } = response.data;
        if (pagination && pagination.cursor) {
            await new Promise((resolve) => setTimeout(resolve, 3000));
            const nextPageData = await this.getAllFollowedStreams(userId, accessToken, pagination.cursor);
            return data.concat(nextPageData);
        } else {
            return data;
        }
    }

    async getStreamByUserId(userId: string): Promise<StreamType | null> {
        try {
            const accessToken = await getAppAccessToken();
            const response = await axios.get(`https://api.twitch.tv/helix/streams?user_id=${userId}`, {
                headers: {
                    Authorization: `Bearer ${accessToken}`,
                    "Client-ID": env.twitchClientId,
                },
            });
            return response.data.data[0] || null;
        } catch (error) {
            logger.error("Error fetching stream details from Twitch API: %s", error);
            throw new Error("Failed to fetch stream details from Twitch API");
        }
    }

    async getAllGames(cursor?: string): Promise<GameArrayType> {
        const accessToken = await getAppAccessToken();
        const response = await axios.get("https://api.twitch.tv/helix/games/top", {
            headers: {
                Authorization: `Bearer ${accessToken}`,
                "Client-ID": env.twitchClientId,
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

    async getGameDetail(gameId: string): Promise<GameType | null> {
        try {
            const accessToken = await getAppAccessToken();
            const response = await axios.get(`https://api.twitch.tv/helix/games?id=${gameId}`, {
                headers: {
                    Authorization: `Bearer ${accessToken}`,
                    "Client-ID": env.twitchClientId,
                },
            });
            return response.data.data[0] || null;
        } catch (error) {
            logger.error("Error fetching game details from Twitch API: %s", error);
            throw new Error("Failed to fetch game details from Twitch API");
        }
    }

    async createEventSub(
        type: string,
        version: string,
        condition: any,
        transport: any
    ): Promise<EventSubType | null> {
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
                        "Client-ID": env.twitchClientId,
                        "Content-Type": "application/json",
                    },
                }
            );
            return response.data;
        } catch (error) {
            handleAxiosError(error);
            throw error;
        }
    }

    async deleteEventSub(id: string) {
        const accessToken = await getAppAccessToken();
        try {
            const response = await axios.delete(`https://api.twitch.tv/helix/eventsub/subscriptions?id=${id}`, {
                headers: {
                    Authorization: `Bearer ${accessToken}`,
                    "Client-ID": env.twitchClientId,
                },
            });

            if (response.status === 204) {
                return "Subscription successfully deleted.";
            } else {
                return response.data;
            }
        } catch (error) {
            if (axios.isAxiosError(error)) {
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
                }
            } else if (error instanceof Error) {
                logger.error(`Non-Axios error: ${error.message}`);
            } else {
                logger.error("An unknown error occurred");
            }
            throw error;
        }
    }

    async getEventSub(cursor?: string): Promise<EventSubType> {
        const accessToken = await getAppAccessToken();
        try {
            const response = await axios.get("https://api.twitch.tv/helix/eventsub/subscriptions", {
                headers: {
                    Authorization: `Bearer ${accessToken}`,
                    "Client-ID": env.twitchClientId,
                },
                params: {
                    first: 100,
                    after: cursor,
                },
            });
            const { data, pagination, total, total_cost, max_total_cost } = response.data;

            if (pagination && pagination.cursor) {
                await new Promise((resolve) => setTimeout(resolve, 3000));
                const nextPageData = await this.getEventSub(pagination.cursor);

                return {
                    total: total + nextPageData.total,
                    data: data.concat(nextPageData.data),
                    total_cost: total_cost + nextPageData.total_cost,
                    max_total_cost: Math.max(max_total_cost, nextPageData.max_total_cost),
                    pagination: nextPageData.pagination,
                };
            } else {
                return {
                    total,
                    data,
                    total_cost,
                    max_total_cost,
                    pagination,
                };
            }
        } catch (error) {
            if (axios.isAxiosError(error)) {
                if (error.response) {
                    logger.error(error.response.data.message);
                }
            } else if (error instanceof Error) {
                logger.error(`Non-Axios error: ${error.message}`);
            } else {
                logger.error("An unknown error occurred");
            }
            throw error;
        }
    }
}

export default TwitchAPI;
