import { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import { env, logger as rootLogger } from "../../app";
import { UserSession } from "../../models/model.user";
const logger = rootLogger.child({ domain: "auth", service: "handler" });

export class AuthHandler {
    async handleTwitchCallback(fastify: FastifyInstance, req: FastifyRequest, reply: FastifyReply): Promise<void> {
        try {
            const { token } = await fastify.twitchOauth2.getAccessTokenFromAuthorizationCodeFlow(req);
            const service = req.server.auth.service;
            const userRepository = req.server.user.repository;

            const userData = await service.fetchTwitchUserData(token.access_token);
            if (env.isWhitelistEnabled && (!userData.id || !env.whitelistedUserIds.includes(userData.id))) {
                logger.error(`User try to connect but are not whitelisted: ${userData.id}`);
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
            await userRepository.updateUserDetail(userData);
            reply.redirect(env.reactUrl);
            await service.initUser(userData.id, token.access_token);
        } catch (err) {
            reply.code(500).send({ error: "Failed to authenticate with Twitch." });
        }
    }

    async checkSession(req: FastifyRequest, reply: FastifyReply): Promise<void> {
        try {
            const userSession = req.session.user;
            const service = req.server.auth.service;
            if (userSession && userSession.twitchToken.refresh_token) {
                if (service.isExpiredToken(userSession.twitchToken.expires_in)) {
                    const refreshToken = userSession.twitchToken.refresh_token;
                    const result = await service.fetchRefreshToken(
                        refreshToken,
                        env.twitchClientId,
                        env.twitchSecret
                    );
                    if (!result) {
                        logger.error("Failed to refresh token");
                        reply.status(401).send({ status: "not authenticated" });
                        return;
                    } else {
                        userSession.twitchToken = { ...userSession.twitchToken, ...result };
                        logger.info("Token refreshed");
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

    async signOut(req: FastifyRequest, reply: FastifyReply): Promise<void> {
        if (req.session) {
            req.session.delete();
            reply.status(200).send({ status: "Successfully signed out" });
        } else {
            reply.status(401).send({ status: "Not authenticated" });
        }
    }

    async getUser(req: FastifyRequest, reply: FastifyReply): Promise<void> {
        if (req.session?.user) {
            // const { accessToken, refreshToken, twitchUserData } = req.session.user;
            const { twitchUserData } = req.session.user;
            reply.send(twitchUserData);
        } else {
            logger.error(`User unauthorized`);
            reply.status(401).send({ error: "Unauthorized" });
        }
    }

    async refreshToken(req: FastifyRequest, reply: FastifyReply): Promise<void> {
        logger.info(`Refreshing token...`);
        const userSession = req.session?.user;
        const service = req.server.auth.service;
        if (userSession && userSession.twitchToken.refresh_token) {
            const refreshToken = userSession.twitchToken.refresh_token;
            const result = await service.fetchRefreshToken(refreshToken, env.twitchClientId, env.twitchSecret);
            if (!result) {
                reply.status(500).send({ error: "Failed to refresh token" });
                return;
            }
            userSession.twitchToken = { ...userSession.twitchToken, ...result };
            reply.status(200).send({ status: "Token refreshed" });
            return;
        } else {
            reply.status(401).send({ error: "Unauthorized" });
        }
    }
}
