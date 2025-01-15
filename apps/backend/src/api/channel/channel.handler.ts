import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "channel", service: "handler" });

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

export class ChannelHandler {
    getChannel = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const broadcasterId = req.params.id;
        const repository = req.server.channel.repository;
        if (!broadcasterId || typeof broadcasterId !== "string") {
            return reply.status(400).send({ message: "Invalid broadcasterId" });
        }
        try {
            const channel = await repository.getChannel(broadcasterId);
            if (!channel) {
                return reply.status(404).send({ message: "Channel not found" });
            }
            reply.send(channel);
        } catch (error) {
            logger.error("Error fetching channel details: %s", error);
            reply.status(500).send("Error fetching channel details");
        }
    };

    getChannelByName = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const broadcasterName = req.params.name?.toLowerCase();
        const repository = req.server.channel.repository;
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
            const user = await repository.getChannelByName(broadcasterName);
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

    getMultipleChannelDB = async (req: FastifyRequest<Query>, reply: FastifyReply) => {
        const queryBroadcasterIds = req.query.userIds;
        const repository = req.server.channel.repository;
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
            const channels = await repository.getMultipleChannelDB(broadcasterIds);
            reply.send(channels);
        } catch (error) {
            logger.error("Error fetching channel details from database: %s", error);
            reply.status(500).send({ message: "Error fetching channel details from database" });
        }
    };

    updateChannel = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const broadcasterId = req.params.id;
        const repository = req.server.channel.repository;
        if (!broadcasterId || typeof broadcasterId !== "string") {
            return reply.status(400).send({ message: "Invalid broadcasterId" });
        }
        try {
            const channel = await repository.updateChannel(broadcasterId);
            reply.send(channel);
        } catch (error) {
            logger.error("Error updating channel details: %s", error);
            reply.status(500).send({ message: "Error updating channel details" });
        }
    };

    getLastLive = async (req: FastifyRequest, reply: FastifyReply) => {
        try {
            const repository = req.server.channel.repository;
            const channels = await repository.getLastLive();
            reply.send(channels);
        } catch (error) {
            logger.error("Error updating channel details: %s", error);
            reply.status(500).send({ message: "Error updating channel details" });
        }
    };
}
