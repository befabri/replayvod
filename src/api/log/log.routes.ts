import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { logHandler } from ".";

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
