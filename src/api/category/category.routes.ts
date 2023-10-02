import { FastifyInstance } from "fastify";
import * as categoryHandler from "./category.handlers";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/", { preHandler: [isUserWhitelisted, userAuthenticated] }, categoryHandler.getCategories);

    done();
}
