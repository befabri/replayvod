import { FastifyInstance } from "fastify";
import * as channelHandler from "./channel.handlers";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
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
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: channelHandler.getChannelDetail,
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
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: channelHandler.updateUserDetail,
    });

    fastify.get("/", {
        schema: {
            querystring: {
                userIds: { type: "array", items: { type: "string" } },
            },
        },
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: channelHandler.getMultipleUserDetailsFromDB,
    });

    fastify.post("/", {
        schema: {
            body: {
                type: "object",
                properties: {
                    userIds: { type: "array", items: { type: "string" } },
                },
                required: ["userIds"],
            },
        },
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: channelHandler.fetchAndStoreUserDetails,
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
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: channelHandler.getChannelDetailByName,
    });

    done();
}
