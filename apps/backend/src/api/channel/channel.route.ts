import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/middleware.auth";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.channel.handler;

    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/:id", {
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "string" },
                },
                required: ["id"],
            },
        },
        handler: handler.getChannel,
    });

    fastify.put("/:id", {
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "string" },
                },
                required: ["id"],
            },
        },
        handler: handler.updateChannel,
    });

    fastify.get("/", {
        schema: {
            querystring: {
                type: "object",
                properties: {
                    userIds: {
                        type: "array",
                        items: { type: "string" },
                    },
                },
            },
            required: ["userIds"],
        },
        handler: handler.getMultipleChannelDB,
    });

    fastify.get("/name/:name", {
        schema: {
            params: {
                type: "object",
                properties: {
                    name: { type: "string" },
                },
                required: ["name"],
            },
        },
        handler: handler.getChannelByName,
    });

    fastify.get("/stream/lastlive", {
        schema: {},
        handler: handler.getLastLive,
    });

    done();
}
