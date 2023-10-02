import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { downloadHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/user/:id", {
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
        handler: downloadHandler.scheduleUser,
    });

    fastify.get("/stream/:id", {
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
        handler: downloadHandler.downloadStream,
    });

    fastify.post("/schedule", {
        schema: {
            body: {
                type: "object",
                properties: {
                    channelName: { type: "string", minLength: 3 },
                    timeBeforeDelete: {
                        type: ["number", "null"],
                        minimum: 10,
                    },
                    viewersCount: {
                        type: ["number", "null"],
                        minimum: 0,
                    },
                    category: { type: "string", minLength: 2 },
                    tag: { type: "string", pattern: "^[a-zA-Z]{2,}(,[a-zA-Z]{2,})*$" },
                    quality: { enum: ["480", "720", "1080"] },
                    isDeleteRediff: { type: "boolean", default: false },
                    hasTags: { type: "boolean", default: false },
                    hasMinView: { type: "boolean", default: false },
                    hasCategory: { type: "boolean", default: false },
                },
                required: [
                    "channelName",
                    "category",
                    "quality",
                    "hasTags",
                    "hasMinView",
                    "hasCategory",
                    "isDeleteRediff",
                ],
            },
        },
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: downloadHandler.scheduleDownload,
    });

    fastify.get("/status/:id", {
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
        handler: downloadHandler.getJobStatus,
    });

    done();
}
