import { FastifyRequest } from "fastify";
import { logger as rootLogger } from "../app";
const logger = rootLogger.child({ service: "userService" });

export const getUserIdFromSession = (req: FastifyRequest): string | null => {
    if (req.session?.user?.twitchUserData && req.session.user.twitchUserData.id) {
        return req.session.user.twitchUserData.id;
    }
    return null;
};

export default {
    getUserIdFromSession,
};
