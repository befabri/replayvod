import { FastifyInstance, FastifyPluginAsync } from "fastify";
import * as videoController from "../controllers/videoController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/play/:id", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "string" },
                },
                required: ["id"],
            },
        },
        handler: videoController.playVideo,
    });

    fastify.get("/all", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: videoController.getVideos,
    });

    fastify.get("/finished", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: videoController.getFinishedVideos,
    });

    fastify.get("/user/:id", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "string" },
                },
                required: ["id"],
            },
        },
        handler: videoController.getChannelVideos,
    });

    fastify.get("/update/missing", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: videoController.generateMissingThumbnail,
    });

    fastify.get("/thumbnail/:login/:filename", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        schema: {
            params: {
                type: "object",
                properties: {
                    login: { type: "string" },
                    filename: { type: "string" },
                },
                required: ["login", "filename"],
            },
        },
        handler: videoController.getThumbnail,
    });
    done();
}
