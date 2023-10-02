import { FastifyInstance } from "fastify";
import * as videoHandler from "./video.handlers";
import { isUserWhitelisted, userAuthenticated } from "@middlewares/authMiddleware";

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
        handler: videoHandler.playVideo,
    });

    fastify.get("/:id", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "integer" },
                },
                required: ["id"],
            },
        },
        handler: videoHandler.getVideo,
    });

    fastify.get("/all", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: videoHandler.getVideos,
    });

    fastify.get("/finished", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: videoHandler.getFinishedVideos,
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
        handler: videoHandler.getChannelVideos,
    });

    fastify.get("/update/missing", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: videoHandler.generateMissingThumbnail,
    });

    // fastify.get("/thumbnail/:login/:filename", {
    //     preHandler: [isUserWhitelisted, userAuthenticated],
    //     schema: {
    //         params: {
    //             type: "object",
    //             properties: {
    //                 login: { type: "string" },
    //                 filename: { type: "string" },
    //             },
    //             required: ["login", "filename"],
    //         },
    //     },
    //     handler: videoHandler.getThumbnail,
    // });
    done();
}
