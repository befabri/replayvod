import { FastifyInstance } from "fastify";
import * as logHandler from "./log.handlers";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/files/:id", {
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "integer" },
                },
                required: ["id"],
            },
        },
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: logHandler.getLog,
    });

    fastify.get("/files", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: logHandler.getLogs,
    });

    fastify.get("/domains/:id", {
        schema: {
            params: {
                type: "object",
                properties: {
                    id: { type: "integer" },
                },
                required: ["id"],
            },
        },
        handler: logHandler.getDomain,
    });

    fastify.get("/domains", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: logHandler.getDomains,
    });

    done();
}
