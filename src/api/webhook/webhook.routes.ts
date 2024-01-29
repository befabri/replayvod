import { FastifyInstance } from "fastify";
import { webhookHandler } from ".";
import { verifyHmacMiddleware } from "../../middlewares/twitchHmacMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.addHook("preHandler", async (request, reply) => {
        await verifyHmacMiddleware(request, reply);
    });

    // fastify.get("/webhooks/test", webhookHandler.test);

    fastify.post("/webhooks/callback", {
        handler: webhookHandler.callbackWebhook,
    });

    done();
}
