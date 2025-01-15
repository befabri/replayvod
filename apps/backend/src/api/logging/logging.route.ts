import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/middleware.auth";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.logging.handler;

    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

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
        handler: handler.getLog,
    });

    fastify.get("/files", {
        handler: handler.getLogs,
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
        handler: handler.getDomain,
    });

    fastify.get("/domains", {
        handler: handler.getDomains,
    });

    done();
}
