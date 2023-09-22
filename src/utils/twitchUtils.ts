import axios from "axios";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ domain: "auth", service: "accessToken" });

const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;
const TWITCH_SECRET = process.env.TWITCH_SECRET;

export const getAppAccessToken = async () => {
    const latestToken = await prisma.appAccessToken.findFirst({
        orderBy: {
            expiresAt: "desc",
        },
    });
    if (latestToken) {
        return latestToken.accessToken;
    }
    try {
        logger.info("Fetching a new access token...");
        const response = await axios.post("https://id.twitch.tv/oauth2/token", null, {
            params: {
                client_id: TWITCH_CLIENT_ID,
                client_secret: TWITCH_SECRET,
                grant_type: "client_credentials",
            },
        });
        const newToken = response.data.access_token;
        const tokenLifetime = response.data.expires_in * 1000;
        const currentTimestamp = new Date();
        const expiresAt = new Date(currentTimestamp.getTime() + tokenLifetime);
        await prisma.appAccessToken.create({
            data: {
                accessToken: newToken,
                expiresAt: expiresAt,
            },
        });
        return newToken;
    } catch (error) {
        logger.error("Error getting app access token:", error);
        throw error;
    }
};
