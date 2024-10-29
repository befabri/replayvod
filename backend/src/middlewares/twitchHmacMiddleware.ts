import { FastifyRequest, FastifyReply } from "fastify";
import { createHmac, timingSafeEqual } from "crypto";
import { env } from "../app";
import { logger as rootLogger } from "../app";
import {
    HMAC_PREFIX,
    TWITCH_MESSAGE_ID,
    TWITCH_MESSAGE_SIGNATURE,
    TWITCH_MESSAGE_TIMESTAMP,
} from "../models/twitchModel";
const logger = rootLogger.child({ domain: "hmac", service: "middleware" });

export const verifyHmacMiddleware = async (req: FastifyRequest, reply: FastifyReply) => {
    let message = getHmacMessage(req);
    let hmac = HMAC_PREFIX + getHmac(env.secret, message);
    let signature = req.headers[TWITCH_MESSAGE_SIGNATURE];
    if (typeof signature !== "string") {
        logger.error("Invalid signature");
        reply.status(400).send("Invalid signature");
        return;
    }
    if (!verifyMessage(hmac, signature)) {
        logger.error("Signature verification failed");
        reply.status(403).send("Signature verification failed");
        return;
    }
};

const getHmacMessage = (req: FastifyRequest): string => {
    const messageId = req.headers[TWITCH_MESSAGE_ID];
    const messageTimestamp = req.headers[TWITCH_MESSAGE_TIMESTAMP];
    if (typeof messageId !== "string" || typeof messageTimestamp !== "string") {
        throw new Error("Invalid message ID or timestamp in headers");
    }
    return messageId + messageTimestamp + JSON.stringify(req.body);
};

const getHmac = (secret: string, message: string): string => {
    return createHmac("sha256", secret).update(message).digest("hex");
};

const verifyMessage = (hmac: string, signature: string): boolean => {
    return timingSafeEqual(Buffer.from(hmac), Buffer.from(signature));
};
