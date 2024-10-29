import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { userHandler } from ".";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/followed-streams", {
        handler: userHandler.getUserFollowedStreams,
    });

    fastify.get("/followed-channels", {
        handler: userHandler.getUserFollowedChannels,
    });

    done();
}
