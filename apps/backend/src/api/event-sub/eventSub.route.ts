import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/middleware.auth";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.eventSub.handler;

    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/subscriptions", {
        handler: handler.getEventSub,
    });

    done();
}
