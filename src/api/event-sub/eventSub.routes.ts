import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { eventSubHandler } from ".";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/subscriptions", {
        handler: eventSubHandler.getListEventSub,
    });

    fastify.get("/costs", {
        handler: eventSubHandler.getTotalCost,
    });

    done();
}
