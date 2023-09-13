import { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import axios from "axios";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "authController" });
import dotenv from "dotenv";
dotenv.config();
const REDIRECT_URL = process.env.REDIRECT_URL || "/";

export async function handleTwitchAuth(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    logger.info("---------------------handleTwitchAuth-------------------------");
}

export async function handleTwitchCallback(
    fastify: FastifyInstance,
    req: FastifyRequest,
    reply: FastifyReply
): Promise<void> {
    logger.info("---------------------handleTwitchCallback-------------------------");
    try {
        const { token } = await fastify.twitchOauth2.getAccessTokenFromAuthorizationCodeFlow(req);
        req.session.set("user", token);
        const res = await saveAppAccessToken(token);
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

//TODO
export async function checkSession(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    const data = req.session.get("user");
    if (req.session?.user) {
        reply.status(200).send({ status: "authenticated" });
        logger.info(`authenticated`);
    } else {
        logger.error(`not authenticated`);
        reply.status(200).send({ status: "not authenticated" });
    }
}

export async function getUser(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    if (req.session?.user) {
        const { accessToken, refreshToken, ...user } = req.session.user;
        logger.info(`user`);
        reply.send(user);
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
