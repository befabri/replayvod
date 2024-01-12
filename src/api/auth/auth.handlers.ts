import { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import axios from "axios";
import { logger as rootLogger } from "../../app";
import { userFeature } from "../user";
import { TwitchTokenResponse, TwitchUserData, UserSession } from "../../models/userModel";
const logger = rootLogger.child({ domain: "auth", service: "authHandler" });

const REACT_URL = process.env.REACT_URL || "/";
const WHITELISTED_USER_IDS: string[] = process.env.WHITELISTED_USER_IDS?.split(",") || [];
const IS_WHITELIST_ENABLED: boolean = process.env.IS_WHITELIST_ENABLED?.toLowerCase() === "true";
const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID || "1"; // TODO
const TWITCH_SECRET = process.env.TWITCH_SECRET || "1"; // TODO

async function fetchTwitchUserData(accessToken: string): Promise<TwitchUserData> {
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
        logger.error("Error fetching Twitch user data: %s", error);
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
        if (IS_WHITELIST_ENABLED && (!userData.id || !WHITELISTED_USER_IDS.includes(userData.id))) {
            logger.error(`User try to connect but are not whitelisted: %s`, userData.id);
            reply.redirect(REACT_URL);
            return;
        }
        const userSessionData: UserSession = {
            twitchToken: {
                access_token: token.access_token,
                expires_in: token.expires_in,
                refresh_token: token.refresh_token,
                token_type: token.token_type,
                expires_at: token.expires_at,
            },
            twitchUserID: userData.id,
            twitchUserData: userData,
        };
        req.session.set("user", userSessionData);
        await userFeature.updateUserDetail(userData);
        reply.redirect(REACT_URL);
        await initUser(userData.id, token.access_token);
    } catch (err) {
        reply.code(500).send({ error: "Failed to authenticate with Twitch." });
    }
}

async function initUser(userId: string, accessToken: string) {
    try {
        await userFeature.getUserFollowedChannels(userId, accessToken);
        await userFeature.getUserFollowedStreams(userId, accessToken);
    } catch (err) {
        logger.error(`Error in initUser`);
    }
}

function isExpiredToken(expires_in: number): boolean {
    const margin = 20 * 60;
    return expires_in <= margin;
}

export async function checkSession(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    try {
        const userSession = req.session?.user as UserSession | undefined;
        if (userSession && userSession.twitchToken.refresh_token) {
            if (isExpiredToken(userSession.twitchToken.expires_in)) {
                const refreshToken = userSession.twitchToken.refresh_token;
                const result = await fetchRefreshToken(refreshToken, TWITCH_CLIENT_ID, TWITCH_SECRET);
                if (!result) {
                    logger.error("Failed to refresh token");
                    reply.status(401).send({ status: "not authenticated" });
                    return;
                } else {
                    req.session.user.twitchToken = { ...req.session.user.twitchToken, ...result };
                    logger.info("Token refreshed");
                    logger.info(req.session.user.twitchToken);
                }
            }
            reply.status(200).send({
                status: "authenticated",
                user: {
                    id: userSession.twitchUserID,
                    login: userSession.twitchUserData.login,
                    display_name: userSession.twitchUserData.display_name,
                    profile_image: userSession.twitchUserData.profile_image_url,
                },
            });
        } else {
            logger.error(`User not authenticated`);
            reply.status(401).send({ status: "not authenticated" });
        }
    } catch (error) {
        logger.error(`Error in checkSession %s`, error);
        reply.status(500).send({ status: "error", message: "Internal Server Error" });
    }
}

export async function signOut(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    if (req.session) {
        req.session.delete();
        reply.status(200).send({ status: "Successfully signed out" });
    } else {
        reply.status(401).send({ status: "Not authenticated" });
    }
}

export async function getUser(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    if (req.session?.user) {
        const { accessToken, refreshToken, twitchUserData } = req.session.user;
        reply.send(twitchUserData);
    } else {
        logger.error(`User unauthorized`);
        reply.status(401).send({ error: "Unauthorized" });
    }
}

export async function refreshToken(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    logger.info(`Refreshing token...`);
    const userSession = req.session?.user as UserSession | undefined;
    if (userSession && userSession.twitchToken.refresh_token) {
        const refreshToken = userSession.twitchToken.refresh_token;
        const result = await fetchRefreshToken(refreshToken, TWITCH_CLIENT_ID, TWITCH_SECRET);
        if (!result) {
            reply.status(500).send({ error: "Failed to refresh token" });
            return;
        }
        req.session.user.twitchToken = { ...req.session.user.twitchToken, ...result };
        reply.status(200).send({ status: "Token refreshed" });
        return;
    } else {
        reply.status(401).send({ error: "Unauthorized" });
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
