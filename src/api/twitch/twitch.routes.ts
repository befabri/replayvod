import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { twitchHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/update/games", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: twitchHandler.fetchAndSaveGames,
    });

    fastify.get("/eventsub/subscriptions", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: twitchHandler.getListEventSub,
    });

    fastify.get("/eventsub/costs", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: twitchHandler.getTotalCost,
    });

    done();
}
