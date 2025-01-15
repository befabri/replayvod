import { FastifyRequest, FastifyReply } from "fastify";
import { env, logger as rootLogger } from "../app";
const logger = rootLogger.child({ domain: "middleware", service: "auth" });

export const isUserWhitelisted = async (req: FastifyRequest, reply: FastifyReply) => {
    const userRepository = req.server.user.repository;
    const userId = userRepository.getUserIdFromSession(req);
    if (env.isWhitelistEnabled && (!userId || !env.whitelistedUserIds.includes(userId))) {
        logger.error(`Forbidden, you're not on the whitelist.`);
        reply.status(403).send({ error: "Forbidden, you're not on the whitelist." });
        throw new Error("Not on the whitelist");
    }
};

export const userAuthenticated = async (req: FastifyRequest, reply: FastifyReply) => {
    if (!req.session?.user) {
        logger.error(`Unauthorized not connected`);
        reply.status(401).send({ error: "Unauthorized" });
        throw new Error("Unauthorized");
    }
};
