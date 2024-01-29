import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { categoryHandler } from ".";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/", categoryHandler.getCategories);
    fastify.get("/videos", categoryHandler.getVideosCategories);
    fastify.get("/videos/done", categoryHandler.getVideosCategoriesDone);
    fastify.get("/detail/:name", {
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
