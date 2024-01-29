import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { logger as rootLogger } from "../../app";
import { channelFeature } from ".";
const logger = rootLogger.child({ domain: "channel", service: "channelHandler" });

const channelCacheNotFound = new Map();
const channelCache = new Map();

interface Params extends RouteGenericInterface {
    Params: {
        id?: string;
        name?: string;
    };
}

interface Query extends RouteGenericInterface {
    Querystring: {
        userIds: string[];
    };
}

export const getChannel = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const broadcasterId = req.params.id;

    if (!broadcasterId || typeof broadcasterId !== "string") {
        return reply.status(400).send({ message: "Invalid broadcasterId" });
    }
    try {
        const channel = await channelFeature.getChannel(broadcasterId);
        if (!channel) {
            return reply.status(404).send({ message: "Channel not found" });
        }
        reply.send(channel);
    } catch (error) {
        logger.error("Error fetching channel details: %s", error);
        reply.status(500).send("Error fetching channel details");
    }
};

export const getChannelByName = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const broadcasterName = req.params.name;
    if (!broadcasterName || typeof broadcasterName !== "string") {
        return reply.status(400).send({ message: "Invalid broadcaster name" });
    }
    try {
        if (channelCacheNotFound.has(broadcasterName)) {
            return reply.send({ exists: false });
        }
        if (channelCache.has(broadcasterName)) {
            return reply.send({ exists: true, user: channelCache.get(broadcasterName) });
        }
        const user = await channelFeature.getChannelByName(broadcasterName);
        if (!user) {
            channelCacheNotFound.set(broadcasterName, true);
            return reply.send({ exists: false });
        }
        channelCache.set(broadcasterName, user);
        reply.send({ exists: true, user });
    } catch (error) {
        logger.error("Error fetching channel details: %s", error);
        reply.status(500).send({ message: "Error fetching channel details" });
    }
};

export const getMultipleChannelDB = async (req: FastifyRequest<Query>, reply: FastifyReply) => {
    const queryBroadcasterIds = req.query.userIds;

    if (!queryBroadcasterIds) {
        return reply.status(400).send({ message: "Invalid 'broadcasterIds' field" });
    }
    let broadcasterIds: string[];
    if (typeof queryBroadcasterIds === "string") {
        broadcasterIds = [queryBroadcasterIds];
    } else if (Array.isArray(queryBroadcasterIds) && typeof queryBroadcasterIds[0] === "string") {
        broadcasterIds = queryBroadcasterIds as string[];
    } else {
        return reply.status(400).send({ message: "Invalid 'broadcasterIds' field" });
    }
    try {
        const channels = await channelFeature.getMultipleChannelDB(broadcasterIds);
        reply.send(channels);
    } catch (error) {
        logger.error("Error fetching channel details from database: %s", error);
        reply.status(500).send({ message: "Error fetching channel details from database" });
    }
};

export const updateChannel = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const broadcasterId = req.params.id;

    if (!broadcasterId || typeof broadcasterId !== "string") {
        return reply.status(400).send({ message: "Invalid broadcasterId" });
    }
    try {
        const channel = await channelFeature.updateChannel(broadcasterId);
        reply.send(channel);
    } catch (error) {
        logger.error("Error updating channel details: %s", error);
        reply.status(500).send({ message: "Error updating channel details" });
    }
};

export const getLastLive = async (_req: FastifyRequest, reply: FastifyReply) => {
    try {
        const channels = await channelFeature.getLastLive();
        reply.send(channels);
    } catch (error) {
        logger.error("Error updating channel details: %s", error);
        reply.status(500).send({ message: "Error updating channel details" });
    }
};
