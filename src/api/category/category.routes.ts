import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { categoryHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/", { preHandler: [isUserWhitelisted, userAuthenticated] }, categoryHandler.getCategories);
    fastify.get(
        "/videos",
        { preHandler: [isUserWhitelisted, userAuthenticated] },
        categoryHandler.getVideosCategories
    );
    fastify.get(
        "/videos/done",
        { preHandler: [isUserWhitelisted, userAuthenticated] },
        categoryHandler.getVideosCategoriesDone
    );

    fastify.get("/detail/:name", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        schema: {
            params: {
                type: "object",
                properties: {
                    name: { type: "string" },
                },
                required: ["name"],
            },
        },
        handler: categoryHandler.getCategory,
    });

    done();
}
