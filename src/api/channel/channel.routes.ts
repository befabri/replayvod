import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { channelHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
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
        handler: channelHandler.getChannel,
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
        handler: channelHandler.updateChannel,
    });

    fastify.get("/", {
        schema: {
            querystring: {
                userIds: { type: "array", items: { type: "string" } },
            },
        },
        handler: channelHandler.getMultipleChannelDB,
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
        handler: channelHandler.getChannelByName,
    });

    fastify.get("/stream/lastlive", {
        schema: {},
        handler: channelHandler.getLastLive,
    });

    done();
}
