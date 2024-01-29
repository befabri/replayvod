import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { videoHandler } from ".";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/play/:id", {
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

    fastify.get("/category/:name", {
        schema: {
            params: {
                type: "object",
                properties: {
                    name: { type: "string" },
                },
                required: ["name"],
            },
        },
        handler: videoHandler.getVideosByCategory,
    });

    fastify.get("/all", {
        handler: videoHandler.getVideos,
    });

    fastify.get("/finished", {
        handler: videoHandler.getFinishedVideos,
    });

    fastify.get("/statistics", {
        handler: videoHandler.getVideoStatistics,
    });

    fastify.get("/channel/:broadcasterLogin", {
        schema: {
            params: {
                type: "object",
                properties: {
                    broadcasterLogin: { type: "string" },
                },
                required: ["broadcasterLogin"],
            },
        },
        handler: videoHandler.getChannelVideos,
    });

    fastify.get("/update/missing", {
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
