import { FastifyInstance } from "fastify";
import * as userController from "../controllers/userController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/me/followedstreams", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userController.getUserFollowedStreams,
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
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userController.getUserDetail,
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
        handler: userController.updateUserDetail,
    });

    fastify.get("/", {
        schema: {
            querystring: {
                userIds: { type: "array", items: { type: "string" } },
            },
        },
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userController.getMultipleUserDetailsFromDB,
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
        handler: userController.fetchAndStoreUserDetails,
    });

    fastify.get("/me/followedchannels", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userController.getUserFollowedChannels,
    });

    fastify.get("/update/users", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userController.updateUsers,
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
        handler: userController.getChannelDetailByName,
    });

    done();
}
