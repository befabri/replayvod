import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { logHandler } from ".";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
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
        handler: logHandler.getLog,
    });

    fastify.get("/files", {
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
        handler: logHandler.getDomains,
    });

    done();
}
