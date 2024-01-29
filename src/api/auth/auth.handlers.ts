import { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import { env, logger as rootLogger } from "../../app";
import { userFeature } from "../user";
import { UserSession } from "../../models/userModel";
import { authFeature } from ".";
const logger = rootLogger.child({ domain: "auth", service: "authHandler" });

export async function handleTwitchCallback(
    fastify: FastifyInstance,
    req: FastifyRequest,
    reply: FastifyReply
): Promise<void> {
    try {
        const { token } = await fastify.twitchOauth2.getAccessTokenFromAuthorizationCodeFlow(req);
        const userData = await authFeature.fetchTwitchUserData(token.access_token);
        if (env.isWhitelistEnabled && (!userData.id || !env.whitelistedUserIds.includes(userData.id))) {
            logger.error(`User try to connect but are not whitelisted: %s`, userData.id);
            reply.redirect(env.reactUrl);
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
        reply.redirect(env.reactUrl);
        await authFeature.initUser(userData.id, token.access_token);
    } catch (err) {
        reply.code(500).send({ error: "Failed to authenticate with Twitch." });
    }
}

export async function checkSession(req: FastifyRequest, reply: FastifyReply): Promise<void> {
    try {
        const userSession = req.session?.user as UserSession | undefined;
        if (userSession && userSession.twitchToken.refresh_token) {
            if (authFeature.isExpiredToken(userSession.twitchToken.expires_in)) {
                const refreshToken = userSession.twitchToken.refresh_token;
                const result = await authFeature.fetchRefreshToken(
                    refreshToken,
                    env.twitchClientId,
                    env.twitchSecret
                );
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
        const result = await authFeature.fetchRefreshToken(refreshToken, env.twitchClientId, env.twitchSecret);
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
