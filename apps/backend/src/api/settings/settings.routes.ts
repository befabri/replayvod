import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { settingsHandler } from ".";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/", {
        schema: {
            querystring: {
                userIds: { type: "array", items: { type: "string" } },
            },
        },
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
        handler: settingsHandler.upsertSettings,
    });

    done();
}
