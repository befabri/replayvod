import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { taskHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    const taskSchema = {
        params: {
            type: "object",
            properties: {
                id: { type: "string" },
            },
            required: ["id"],
        },
    };

    fastify.get("/", { preHandler: [isUserWhitelisted, userAuthenticated] }, taskHandler.getTasks);

    fastify.get(
        "/:id",
        {
            preHandler: [isUserWhitelisted, userAuthenticated],
            schema: taskSchema,
        },
        taskHandler.getTask
    );

    fastify.get(
        "/run/:id",
        {
            preHandler: [isUserWhitelisted, userAuthenticated],
            schema: taskSchema,
        },
        taskHandler.runTask
    );

    done();
}
