import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { logger as rootLogger } from "../../app";
import { channelService } from ".";
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

interface Body extends RouteGenericInterface {
    Body: {
        userIds: string[];
    };
}

export const getChannelDetail = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const broadcasterId = req.params.id;

    if (!broadcasterId || typeof broadcasterId !== "string") {
        reply.status(400).send("Invalid broadcasterId");
        return;
    }
    try {
        const channel = await channelService.getChannelDetailDB(broadcasterId);
        if (!channel) {
            reply.status(404).send("Channel not found");
            return;
        }
        reply.send(channel);
    } catch (error) {
        logger.error("Error fetching channel details: %s", error);
        reply.status(500).send("Error fetching channel details");
    }
};

//Backend
export const getChannelDetailByName = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const broadcasterName = req.params.name;
    if (!broadcasterName || typeof broadcasterName !== "string") {
        reply.status(400).send({ error: "Invalid broadcaster name" });
        return;
    }
    try {
        if (channelCacheNotFound.has(broadcasterName)) {
            reply.send({ exists: false });
            return;
        }
        if (channelCache.has(broadcasterName)) {
            reply.send({ exists: true, user: channelCache.get(broadcasterName) });
            return;
        }
        const user = await channelService.getChannelDetailByName(broadcasterName);
        if (!user) {
            channelCacheNotFound.set(broadcasterName, true);
            reply.send({ exists: false });
            return;
        }
        channelCache.set(broadcasterName, user);
        reply.send({ exists: true, user });
    } catch (error) {
        logger.error("Error fetching channel details: %s", error);
        reply.status(500).send({ error: "Error fetching channel details" });
    }
};

export const getMultipleUserDetailsFromDB = async (req: FastifyRequest<Query>, reply: FastifyReply) => {
    const queryBroadcasterIds = req.query.userIds;

    if (!queryBroadcasterIds) {
        reply.status(400).send("Invalid 'broadcasterIds' field");
        return;
    }
    let broadcasterIds: string[];
    if (typeof queryBroadcasterIds === "string") {
        broadcasterIds = [queryBroadcasterIds];
    } else if (Array.isArray(queryBroadcasterIds) && typeof queryBroadcasterIds[0] === "string") {
        broadcasterIds = queryBroadcasterIds as string[];
    } else {
        reply.status(400).send("Invalid 'broadcasterIds' field");
        return;
    }
    try {
        const channels = await channelService.getMultipleChannelDetailsDB(broadcasterIds);
        reply.send(channels);
    } catch (error) {
        logger.error("Error fetching channel details from database: %s", error);
        reply.status(500).send("Error fetching channel details from database");
    }
};

export const updateChannelDetail = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const broadcasterId = req.params.id;

    if (!broadcasterId || typeof broadcasterId !== "string") {
        reply.status(400).send("Invalid broadcasterId");
        return;
    }
    try {
        const channel = await channelService.updateChannelDetail(broadcasterId);
        reply.send(channel);
    } catch (error) {
        logger.error("Error updating channel details: %s", error);
        reply.status(500).send("Error updating channel details");
    }
};

export const fetchAndStoreChannelDetails = async (req: FastifyRequest<Body>, reply: FastifyReply) => {
    const broadcasterIds = req.body.userIds;
    if (!Array.isArray(broadcasterIds) || !broadcasterIds.every((id) => typeof id === "string")) {
        reply.status(400).send("Invalid 'broadcasterIds' field");
        return;
    }
    try {
        const message = await channelService.fetchAndStoreChannelDetails(broadcasterIds);
        reply.status(200).send(message);
    } catch (error) {
        logger.error("Error fetching and storing channel details: %s", error);
        reply.status(500).send("Error fetching and storing channel details");
    }
};
