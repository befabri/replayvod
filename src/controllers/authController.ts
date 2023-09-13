import { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import axios from "axios";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "authController" });
import dotenv from "dotenv";
import { userService } from "../services";
dotenv.config();
const REDIRECT_URL = process.env.REDIRECT_URL || "/";

async function fetchTwitchUserData(accessToken: string) {
    const headers = {
        "Client-ID": process.env.TWITCH_CLIENT_ID,
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
        logger.error("Error fetching Twitch user data:", error);
        throw error;
    }
}

export async function handleTwitchCallback(
    fastify: FastifyInstance,
    req: FastifyRequest,
    reply: FastifyReply
): Promise<void> {
    try {
        const { token } = await fastify.twitchOauth2.getAccessTokenFromAuthorizationCodeFlow(req);
        const userData = await fetchTwitchUserData(token.access_token);
        req.session.set("user", {
            ...token,
            twitchUserID: userData.id,
            twitchUserData: userData,
        });
        const res = await saveAppAccessToken(token);
        const resUser = await userService.updateUserDetail(userData);
        reply.redirect(REDIRECT_URL);
    } catch (err) {
        reply.code(500).send({ error: "Failed to authenticate with Twitch." });
    }
}

export const saveAppAccessToken = async (accessToken) => {
    logger.info("Saving access token...");
    try {
        const newToken = accessToken.access_token;
        const tokenLifetime = accessToken.expires_in * 1000;
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
        logger.error("Error saving app access token:", error);
        throw error;
    }
};

export async function checkSession(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    try {
        if (req.session?.user) {
            logger.info(`User authenticated: ${req.session.user.twitchUserID}`);
            reply.status(200).send({
                status: "authenticated",
                user: { id: req.session.user.twitchUserID },
            });
        } else {
            logger.error(`User not authenticated`);
            reply.status(401).send({ status: "not authenticated" });
        }
    } catch (error) {
        logger.error(`Error in checkSession: ${error.message}`);
        reply.status(500).send({ status: "error", message: "Internal Server Error" });
    }
}

export async function getUser(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    if (req.session?.user) {
        const { accessToken, refreshToken, twitchUserData } = req.session.user;
        reply.send(twitchUserData);
    } else {
        logger.error(`Unauthorized`);
        reply.status(401).send({ error: "Unauthorized" });
    }
}

export async function refreshToken(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    logger.info(`Refreshing token...`);
    if (req.session?.user?.refreshToken) {
        const refreshToken = req.session.user.refreshToken;
        const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;
        const TWITCH_SECRET = process.env.TWITCH_SECRET;

        try {
            const response = await axios({
                method: "post",
                url: "https://id.twitch.tv/oauth2/token",
                params: {
                    grant_type: "refresh_token",
                    refresh_token: refreshToken,
                    client_id: TWITCH_CLIENT_ID,
                    client_secret: TWITCH_SECRET,
                },
            });

            if (response.status === 200) {
                req.session.user.accessToken = response.data.access_token;
                req.session.user.refreshToken = response.data.refresh_token;

                reply.status(200).send({ status: "Token refreshed" });
            } else {
                reply.status(500).send({ error: "Failed to refresh token" });
            }
        } catch (error) {
            logger.error(`Failed to refresh token: ${error}`);
            reply.status(500).send({ error: "Failed to refresh token" });
        }
    } else {
        reply.status(401).send({ error: "Unauthorized" });
    }
}
