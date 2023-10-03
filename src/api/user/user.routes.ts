import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/authMiddleware";
import { userHandler } from ".";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/followedstreams", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userHandler.getUserFollowedStreams,
    });

    fastify.get("/followedchannels", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userHandler.getUserFollowedChannels,
    });

    // fastify.get("/update/users", {
    //     preHandler: [isUserWhitelisted, userAuthenticated],
    //     handler: userHandler.updateUsers,
    // });
    done();
}
