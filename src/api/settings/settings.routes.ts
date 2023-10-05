import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { settingsHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/", {
        schema: {
            querystring: {
                userIds: { type: "array", items: { type: "string" } },
            },
        },
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: settingsHandler.getSettings,
    });

    fastify.post("/", {
        schema: {
            body: {
                type: "object",
                properties: {
                    timeZone: { type: "string" },
                    dateTimeFormat: { type: "string" },
                },
                required: ["timeZone", "dateTimeFormat"],
            },
        },
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: settingsHandler.upsertSettings,
    });

    done();
}
