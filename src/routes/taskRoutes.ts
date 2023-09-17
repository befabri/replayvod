// routes/taskRoutes.ts
import { FastifyInstance } from "fastify";
import * as taskController from "../controllers/taskController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

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

    fastify.get("/", { preHandler: [isUserWhitelisted, userAuthenticated] }, taskController.getTasks);

    fastify.get(
        "/:id",
        {
            preHandler: [isUserWhitelisted, userAuthenticated],
            schema: taskSchema,
        },
        taskController.getTask
    );

    fastify.get(
        "/run/:id",
        {
            preHandler: [isUserWhitelisted, userAuthenticated],
            schema: taskSchema,
        },
        taskController.runTask
    );

    done();
}
