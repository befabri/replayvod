import { FastifyRequest } from "fastify";
import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { SessionUser } from "../../models/userModel";
import { transformSessionUser } from "./user.requestDTO";
const logger = rootLogger.child({ domain: "auth", service: "userService" });

export const getUserIdFromSession = (req: FastifyRequest): string | null => {
    if (req.session?.user?.twitchUserData && req.session.user.twitchUserData.id) {
        return req.session.user.twitchUserData.id;
    }
    return null;
};

export const getUserAccessTokenFromSession = (req: FastifyRequest): string | null => {
    if (req.session?.user && req.session.user.access_token) {
        return req.session.user.access_token;
    }
    return null;
};

export const updateUserDetail = async (userData: SessionUser) => {
    const user = await transformSessionUser(userData);
    if (user) {
        try {
            await prisma.user.upsert({
                where: { userId: user.userId },
                update: user,
                create: user,
            });
        } catch (error) {
            logger.error("Error updating/inserting user: %s", error);
        }
    }
    return user;
};
