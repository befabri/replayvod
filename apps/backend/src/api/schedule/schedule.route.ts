import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/middleware.auth";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.schedule.handler;

    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.post("/", {
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
                    categories: {
                        type: "array",
                        items: { type: "string" },
                    },
                    tags: {
                        type: "array",
                        items: { type: "string" },
                    },
                    quality: { enum: ["480", "720", "1080"] },
                    isDeleteRediff: { type: "boolean", default: false },
                    hasTags: { type: "boolean", default: false },
                    hasMinView: { type: "boolean", default: false },
                    hasCategory: { type: "boolean", default: false },
                },
                required: [
                    "channelName",
                    "categories",
                    "quality",
                    "hasTags",
                    "hasMinView",
                    "hasCategory",
                    "isDeleteRediff",
                ],
            },
        },
        handler: handler.createSchedule,
    });

    fastify.put("/:id", {
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "integer" },
                },
                required: ["id"],
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
                    categories: {
                        type: "array",
                        items: { type: "string" },
                    },
                    tags: {
                        type: "array",
                        items: { type: "string" },
                    },
                    quality: { enum: ["480", "720", "1080"] },
                    isDeleteRediff: { type: "boolean", default: false },
                    hasTags: { type: "boolean", default: false },
                    hasMinView: { type: "boolean", default: false },
                    hasCategory: { type: "boolean", default: false },
                },
                required: [
                    "channelName",
                    "categories",
                    "quality",
                    "hasTags",
                    "hasMinView",
                    "hasCategory",
                    "isDeleteRediff",
                ],
            },
        },
        handler: handler.editSchedule,
    });

    fastify.delete("/:id", {
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "integer" },
                },
                required: ["id"],
            },
        },
        handler: handler.removeSchedule,
    });

    fastify.post("/:id/toggle", {
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "integer" },
                },
                required: ["id"],
            },
            body: {
                type: "object",
                properties: {
                    enable: { type: "boolean" },
                },
                required: ["enable"],
            },
        },
        handler: handler.toggleScheduleStatus,
    });

    fastify.get("/", {
        handler: handler.getCurrentSchedules,
    });

    done();
}
