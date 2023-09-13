import { FastifyInstance } from "fastify";
import * as twitchAPIController from "../controllers/twitchAPIController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/update/games", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: twitchAPIController.fetchAndSaveGames,
    });

    fastify.get("/eventsub/subscriptions", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: twitchAPIController.getListEventSub,
    });

    fastify.get("/eventsub/costs", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: twitchAPIController.getTotalCost,
    });

    done();
}
