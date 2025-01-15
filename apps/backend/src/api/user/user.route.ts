import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/middleware.auth";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.user.handler;

    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/followed-streams", {
        handler: handler.getUserFollowedStreams,
    });

    fastify.get("/followed-channels", {
        handler: handler.getUserFollowedChannels,
    });

    done();
}
