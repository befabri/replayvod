import { FastifyReply, FastifyRequest } from "fastify";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "event-sub", service: "handler" });

export class EventSubHandler {
    // fetchAndSaveGames = async (req: FastifyRequest, reply: FastifyReply) => {
    //     try {
    //         const service = req.server.eventSub.service;
    //         const categories = await twitchService.getAllGames();
    //         if (!categories || categories == undefined) {
    //             return reply.status(500).send("Error getting all games");
    //         }
    //         await service.addAllCategories(categories);
    //         reply.send({ message: "Games fetched and saved successfully." });
    //     } catch (err) {
    //         logger.error(err);
    //         reply.status(500).send({ message: "An error occurred while fetching and saving games." });
    //     }
    // };

    getEventSub = async (req: FastifyRequest, reply: FastifyReply) => {
        const service = req.server.eventSub.service;
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        try {
            const { data, message } = await service.getEventSub(userId);
            return reply.send({ data: data, message: message });
        } catch (err) {
            logger.error(err);
            reply.status(500).send({ message: "An error occurred while fetching EventSub subscriptions." });
        }
    };
}
