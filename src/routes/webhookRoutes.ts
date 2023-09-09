import { FastifyInstance } from "fastify";
import * as webhookController from "../controllers/webhookController";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.post("/webhooks", webhookController.addWebhook);
    fastify.delete("/webhooks", webhookController.removeWebhook);
    fastify.post("/webhooks/callback", webhookController.callbackWebhook);

    done();
}
