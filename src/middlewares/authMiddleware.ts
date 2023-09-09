import { FastifyRequest, FastifyReply, HookHandlerDoneFunction } from "fastify";
import dotenv from "dotenv";

dotenv.config();

const WHITELISTED_USER_IDS: string[] = process.env.WHITELISTED_USER_IDS?.split(",") || [];
const IS_WHITELIST_ENABLED: boolean = process.env.IS_WHITELIST_ENABLED?.toLowerCase() === "true";

export async function isUserWhitelisted(req: FastifyRequest, reply: FastifyReply, done: HookHandlerDoneFunction) {
    const userID = req.session?.passport?.user?.twitchId;
    if (!IS_WHITELIST_ENABLED || (userID && WHITELISTED_USER_IDS.includes(userID))) {
        done();
    } else {
        reply.status(403).send({ error: "Forbidden, you're not on the whitelist." });
    }
}

export async function userAuthenticated(req: FastifyRequest, reply: FastifyReply, done: HookHandlerDoneFunction) {
    if (req.session?.passport?.user) {
        done();
    } else {
        reply.status(401).send({ error: "Unauthorized" });
    }
}
