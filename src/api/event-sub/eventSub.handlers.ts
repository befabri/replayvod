import { FastifyReply, FastifyRequest } from "fastify";
import { logger as rootLogger } from "../../app";
import { userFeature } from "../user";
import { categoryFeature } from "../category";
import { eventSubFeature } from "../webhook";
import { twitchService } from "../../services";
const logger = rootLogger.child({ domain: "twitch", service: "twitchHandler" });

export const fetchAndSaveGames = async (req: FastifyRequest, reply: FastifyReply) => {
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

export const getListEventSub = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    try {
        const eventSub = await eventSubFeature.getEventSub(userId);
        if ("data" in eventSub && "message" in eventSub) {
            return reply.send({ data: eventSub.data, message: eventSub.message });
        }
        reply.send(eventSub);
    } catch (err) {
        logger.error(err);
        reply.status(500).send({ message: "An error occurred while fetching EventSub subscriptions." });
    }
};

export const getTotalCost = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const eventSub = await eventSubFeature.getTotalCost();
        reply.send({ data: eventSub.data, message: eventSub.message });
    } catch (err) {
        logger.error(err);
        reply.status(500).send({ message: "An error occurred while fetching total cost." });
    }
};
