import { FastifyReply, FastifyRequest } from "fastify";
import { logger as rootLogger } from "../../app";
import { userFeature } from "../user";
import { categoryFeature } from "../category";
import { twitchService } from "../../services";
import { eventSubFeature } from ".";
const logger = rootLogger.child({ domain: "twitch", service: "twitchHandler" });

export const fetchAndSaveGames = async (_req: FastifyRequest, reply: FastifyReply) => {
    try {
        const categories = await twitchService.getAllGames();
        if (!categories || categories == undefined) {
            return reply.status(500).send("Error getting all games");
        }
        await categoryFeature.addAllCategories(categories);
        reply.send({ message: "Games fetched and saved successfully." });
    } catch (err) {
        logger.error(err);
        reply.status(500).send({ message: "An error occurred while fetching and saving games." });
    }
};

export const getEventSub = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    try {
        const { data, message } = await eventSubFeature.getEventSub(userId);
        return reply.send({ data: data, message: message });
    } catch (err) {
        logger.error(err);
        reply.status(500).send({ message: "An error occurred while fetching EventSub subscriptions." });
    }
};
