import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/middleware.auth";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.video.handler;

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
        handler: handler.playVideo,
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
        handler: handler.getVideo,
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
        handler: handler.getVideosByCategory,
    });

    fastify.get("/all", {
        handler: handler.getVideos,
    });

    fastify.get("/finished", {
        handler: handler.getFinishedVideos,
    });

    fastify.get("/pending", {
        handler: handler.getPendingVideos,
    });

    fastify.get("/statistics", {
        handler: handler.getVideoStatistics,
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
        handler: handler.getChannelVideos,
    });

    fastify.get("/update/missing", {
        handler: handler.generateMissingThumbnail,
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
