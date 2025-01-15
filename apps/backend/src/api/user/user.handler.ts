import { FastifyRequest, FastifyReply } from "fastify";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "user", service: "handler" });

export class UserHandler {
    getUserFollowedStreams = async (req: FastifyRequest, reply: FastifyReply) => {
        const repository = req.server.user.repository;
        const userId = repository.getUserIdFromSession(req);
        const accessToken = repository.getUserAccessTokenFromSession(req);
        if (!userId || !accessToken) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        try {
            const followedStreams = await repository.getUserFollowedStreams(userId, accessToken);
            reply.send(followedStreams);
        } catch (error) {
            logger.error("Error fetching followed streams:", error);
            reply.status(500).send({ message: "Error fetching followed streams" });
        }
    };

    getUserFollowedChannels = async (req: FastifyRequest, reply: FastifyReply) => {
        const repository = req.server.user.repository;
        const userId = repository.getUserIdFromSession(req);
        const accessToken = repository.getUserAccessTokenFromSession(req);
        if (!userId || !accessToken) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        try {
            const followedChannels = await repository.getUserFollowedChannels(userId, accessToken);
            reply.send(followedChannels);
        } catch (error) {
            logger.error("Error fetching followed channels:", error);
            reply.status(500).send({ message: "Error fetching followed channels" });
        }
    };
}
