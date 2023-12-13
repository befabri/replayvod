import { FastifyRequest, FastifyReply } from "fastify";
import { logger as rootLogger } from "../app";
import { userService } from "../api/user";
const logger = rootLogger.child({ domain: "auth", service: "authMiddleware" });

const WHITELISTED_USER_IDS: string[] = process.env.WHITELISTED_USER_IDS?.split(",") || [];
const IS_WHITELIST_ENABLED: boolean = process.env.IS_WHITELIST_ENABLED?.toLowerCase() === "true";

export const isUserWhitelisted = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    if (IS_WHITELIST_ENABLED && (!userId || !WHITELISTED_USER_IDS.includes(userId))) {
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
