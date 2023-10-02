import { FastifyInstance } from "fastify";
import * as userHandler from "./user.handlers";
import { isUserWhitelisted, userAuthenticated } from "@middlewares/authMiddleware";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get("/me/followedstreams", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userHandler.getUserFollowedStreams,
    });

    fastify.get("/me/followedchannels", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userHandler.getUserFollowedChannels,
    });

    fastify.get("/update/users", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: userHandler.updateUsers,
    });

    done();
}
