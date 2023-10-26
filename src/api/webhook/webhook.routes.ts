import { FastifyInstance } from "fastify";
import { webhookHandler } from ".";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    // fastify.get("/webhooks/test", webhookHandler.test);

    fastify.post("/webhooks", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: webhookHandler.addWebhook,
    });

    fastify.delete("/webhooks", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: webhookHandler.removeWebhook,
    });

    fastify.post("/webhooks/callback", {
        handler: webhookHandler.callbackWebhook,
    });

    done();
}
