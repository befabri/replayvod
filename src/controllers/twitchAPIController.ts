import { FastifyReply, FastifyRequest } from "fastify";
import TwitchAPI from "../utils/twitchAPI";
import { eventSubService, gameService } from "../services";

const twitchAPI = new TwitchAPI();

export const fetchAndSaveGames = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const games = await twitchAPI.getAllGames();
        await gameService.saveGamesToDb(games);
        reply.send({ message: "Games fetched and saved successfully." });
    } catch (err) {
        console.error(err);
        reply.status(500).send({ error: "An error occurred while fetching and saving games." });
    }
};

export const getListEventSub = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = req.session?.passport?.user?.data[0]?.id;
    if (!userId || userId == undefined) {
        reply.status(500).send("Error no user authenticated");
        return;
    }
    try {
        const eventSub = await eventSubService.getEventSub(userId);
        reply.send({ data: eventSub.data, message: eventSub.message });
    } catch (err) {
        console.error(err);
        reply.status(500).send({ error: "An error occurred while fetching EventSub subscriptions." });
    }
};

export const getTotalCost = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const eventSub = await eventSubService.getTotalCost();
        reply.send({ data: eventSub.data, message: eventSub.message });
    } catch (err) {
        console.error(err);
        reply.status(500).send({ error: "An error occurred while fetching total cost." });
    }
};
