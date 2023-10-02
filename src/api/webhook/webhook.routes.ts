import { FastifyInstance } from "fastify";
import * as webhookHandler from "./webhook.handlers";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.post("/webhooks", webhookHandler.addWebhook);
    fastify.delete("/webhooks", webhookHandler.removeWebhook);
    fastify.post("/webhooks/callback", webhookHandler.callbackWebhook);

    done();
}
