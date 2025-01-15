import { FastifyInstance } from "fastify";
import { verifyHmacMiddleware } from "../../middlewares/middleware.hmac";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.webhook.handler;

    fastify.addHook("preHandler", async (request, reply) => {
        await verifyHmacMiddleware(request, reply);
    });

    // fastify.get("/webhooks/test", webhookHandler.test);

    fastify.post("/webhooks/callback", {
        handler: handler.callbackWebhook,
    });

    done();
}
