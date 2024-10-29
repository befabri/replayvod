import axios from "axios";
import { env, logger as rootLogger } from "../../app";
import { prisma } from "../../server";
const logger = rootLogger.child({ domain: "auth", service: "accessToken" });

if (!env.twitchClientId || !env.twitchSecret) {
    throw new Error("Missing .env: env.twitchClientId and/or env.twitchSecret");
}

export const getAppAccessToken = async () => {
    const latestToken = await prisma.appAccessToken.findFirst({
        orderBy: {
            expiresAt: "desc",
        },
    });
    if (latestToken && new Date(latestToken.expiresAt) > new Date(Date.now() + 300000)) {
        // 5 min
        return latestToken.accessToken;
    }

    try {
        const token = await fetchAppAccessToken();
        await saveAppAccessToken(token.access_token, token.expires_in);
        return token.access_token;
    } catch (error) {
        logger.error("Error getting app access token: %s", error);
        throw error;
    }
};

export const fetchAppAccessToken = async () => {
    logger.info("Fetching access token...");
    try {
        const response = await axios.post("https://id.twitch.tv/oauth2/token", null, {
            params: {
                client_id: env.twitchClientId,
                client_secret: env.twitchSecret,
                grant_type: "client_credentials",
            },
        });
        return response.data;
    } catch (error) {
        logger.error("Error fetching app access token: %s", error);
        throw error;
    }
};

export const saveAppAccessToken = async (accessToken: string, expiresIn: number) => {
    logger.info("Saving access token...");
    try {
        const tokenLifetime = expiresIn * 1000;
        const currentTimestamp = new Date();
        const expiresAt = new Date(currentTimestamp.getTime() + tokenLifetime);
        await prisma.appAccessToken.create({
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
export const cleanupExpiredTokens = async () => {
    const currentTimestamp = new Date();
    await prisma.appAccessToken.deleteMany({
        where: {
            expiresAt: {
                lte: currentTimestamp,
            },
        },
    });
};
