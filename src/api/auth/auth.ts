import axios from "axios";
import { env, logger as rootLogger } from "../../app";
import { userFeature } from "../user";
import { TwitchTokenResponse, TwitchUserData } from "../../models/userModel";
const logger = rootLogger.child({ domain: "auth", service: "authHandler" });

export async function fetchTwitchUserData(accessToken: string): Promise<TwitchUserData> {
    const headers = {
        "Client-ID": env.twitchClientId,
        Authorization: `Bearer ${accessToken}`,
    };
    try {
        const response = await axios.get("https://api.twitch.tv/helix/users", { headers });
        if (response.data && response.data.data && response.data.data.length > 0) {
            return response.data.data[0]; // Twitch returns an array with a single user object
        } else {
            throw new Error("Twitch user data not found.");
        }
    } catch (error) {
        logger.error("Error fetching Twitch user data: %s", error);
        throw error;
    }
}

export async function fetchRefreshToken(
    refreshToken: string,
    clientId: string,
    twitchSecret: string
): Promise<TwitchTokenResponse | null> {
    try {
        const response = await axios({
            method: "post",
            url: "https://id.twitch.tv/oauth2/token",
            params: {
                grant_type: "refresh_token",
                refresh_token: refreshToken,
                client_id: clientId,
                client_secret: twitchSecret,
            },
        });

        if (response.status === 200) {
            return response.data;
        } else {
            logger.error(`Failed to refresh token, response from Twitch API not 200`);
            return null;
        }
    } catch (error) {
        logger.error(`Failed to refresh token: ${error}`);
        return null;
    }
}

export async function initUser(userId: string, accessToken: string) {
    try {
        await userFeature.getUserFollowedChannels(userId, accessToken);
        await userFeature.getUserFollowedStreams(userId, accessToken);
    } catch (err) {
        logger.error(`Error in initUser`);
    }
}

export function isExpiredToken(expires_in: number): boolean {
    const margin = 20 * 60;
    return expires_in <= margin;
}
