import { FastifyReply, FastifyRequest } from "fastify";
import * as categoryService from "@api/category";
import * as twitchService from "./twitch";
import * as userService from "@api/user";
import * as eventSubService from "@api/webhook/eventSubService";

export const fetchAndSaveGames = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const categories = await twitchService.getAllGames();
        await categoryService.addAllCategories(categories);
        reply.send({ message: "Games fetched and saved successfully." });
    } catch (err) {
        console.error(err);
        reply.status(500).send({ error: "An error occurred while fetching and saving games." });
    }
};

export const getListEventSub = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    if (!userId || userId == undefined) {
        reply.status(500).send("Error no user authenticated");
        return;
    }
    try {
        const eventSub = await eventSubService.getEventSub(userId);
        if ("data" in eventSub && "message" in eventSub) {
            reply.send({ data: eventSub.data, message: eventSub.message });
        } else {
            reply.send(eventSub);
        }
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
