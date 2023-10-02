import { FastifyRequest, FastifyReply } from "fastify";
import * as channelService from "../channel";
import * as userService from "./user";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "user", service: "userHandler" });

export const getUserFollowedStreams = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    const accessToken = userService.getUserAccessTokenFromSession(req);
    if (!userId || !accessToken) {
        reply.status(401).send("Unauthorized");
        return;
    }
    try {
        const followedStreams = await channelService.getUserFollowedStreams(userId, accessToken);
        reply.send(followedStreams);
    } catch (error) {
        logger.error("Error fetching followed streams:", error);
        reply.status(500).send("Error fetching followed streams");
    }
};

export const getUserFollowedChannels = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    const accessToken = userService.getUserAccessTokenFromSession(req);
    if (!userId || !accessToken) {
        reply.status(401).send("Unauthorized");
        return;
    }
    try {
        const followedChannels = await channelService.getUserFollowedChannels(userId, accessToken);
        reply.send(followedChannels);
    } catch (error) {
        logger.error("Error fetching followed channels:", error);
        reply.status(500).send("Error fetching followed channels");
    }
};

// TODO
export const updateUsers = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    const accessToken = userService.getUserAccessTokenFromSession(req);
    if (!userId || !accessToken) {
        reply.status(401).send("Unauthorized");
        return;
    }
    try {
        // const result = await channelService.updateUsers(userId);
        // reply.status(200).send(result);
        reply.status(200).send(null);
    } catch (error) {
        logger.error("Error updating users:", error);
        reply.status(500).send("Error updating users");
    }
};
