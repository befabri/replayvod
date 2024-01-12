import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { downloadHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
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
        handler: downloadHandler.scheduleDownload,
    });

    fastify.put("/schedule/:scheduleId", {
        schema: {
            params: {
                type: "object",
                properties: {
                    scheduleId: { type: "integer" },
                },
                required: ["scheduleId"],
            },
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
        handler: downloadHandler.scheduleDownloadEdit,
    });

    fastify.delete("/schedule/:scheduleId", {
        schema: {
            params: {
                type: "object",
                properties: {
                    scheduleId: { type: "integer" },
                },
                required: ["scheduleId"],
            },
        },
        handler: downloadHandler.scheduleRemoved,
    });

    fastify.post("/schedule/:scheduleId/toggle", {
        schema: {
            params: {
                type: "object",
                properties: {
                    scheduleId: { type: "integer" },
                },
                required: ["scheduleId"],
            },
            body: {
                type: "object",
                properties: {
                    enable: { type: "boolean" },
                },
                required: ["enable"],
            },
        },
        handler: downloadHandler.scheduleToggle,
    });

    fastify.get("/schedule", {
        handler: downloadHandler.getCurrentSchedule,
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
        handler: downloadHandler.getJobStatus,
    });

    done();
}
