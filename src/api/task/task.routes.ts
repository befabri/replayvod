import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { taskHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/", { preHandler: [isUserWhitelisted, userAuthenticated] }, taskHandler.getTasks);

    fastify.get("/:id", {
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
        handler: taskHandler.getTask,
    });

    fastify.get("/run/:id", {
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
        handler: taskHandler.runTask,
    });

    done();
}
