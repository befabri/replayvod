import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { userHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.addHook("preHandler", async (request, reply) => {
        await isUserWhitelisted(request, reply);
        await userAuthenticated(request, reply);
    });

    fastify.get("/followedstreams", {
        handler: userHandler.getUserFollowedStreams,
    });

    fastify.get("/followedchannels", {
        handler: userHandler.getUserFollowedChannels,
    });

    // fastify.get("/update/users", {
    //     preHandler: [isUserWhitelisted, userAuthenticated],
    //     handler: userHandler.updateUsers,
    // });
    done();
}
