import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/middleware.auth";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.settings.handler;

    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/", {
        schema: {},
        handler: handler.getSettings,
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
        handler: handler.upsertSettings,
    });

    done();
}
