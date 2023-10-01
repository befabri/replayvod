// routes/taskRoutes.ts
import { FastifyInstance } from "fastify";
import * as categoryController from "../controllers/categoryController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/", { preHandler: [isUserWhitelisted, userAuthenticated] }, categoryController.getCategories);

    done();
}
