import { FastifyInstance } from "fastify";
import * as downloadController from "../controllers/downloadController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

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
        handler: downloadController.scheduleUser,
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
        handler: downloadController.downloadStream,
    });

    fastify.post("/schedule", {
        schema: {
            body: {
                type: "object",
                properties: {
                    source: { type: "string" },
                    channelName: { type: "string" },
                    viewersCount: { type: "number" },
                    timeBeforeDelete: { type: "number" },
                    trigger: { type: "string" },
                    tag: { type: "string" },
                    category: { type: "string" },
                    quality: { type: "string" },
                    isDeleteRediff: { type: "boolean" },
                    requested_by: { type: "string" },
                },
                required: [
                    "source",
                    "channelName",
                    "viewersCount",
                    "timeBeforeDelete",
                    "trigger",
                    "tag",
                    "category",
                    "quality",
                    "isDeleteRediff",
                    "requested_by",
                ],
            },
        },
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: downloadController.scheduleDownload,
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
        handler: downloadController.getJobStatus,
    });

    done();
}
