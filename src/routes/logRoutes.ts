import { FastifyInstance } from "fastify";
import { getLog, getLogs, getDomain, getDomains } from "../controllers/logController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

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
        handler: getLog,
    });

    fastify.get("/files", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: getLogs,
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
        handler: getDomain,
    });

    fastify.get("/domains", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: getDomains,
    });

    done();
}
