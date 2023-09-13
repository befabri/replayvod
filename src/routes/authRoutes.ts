import { FastifyInstance, FastifyPluginAsync } from "fastify";
import { handleTwitchCallback, checkSession, getUser, refreshToken } from "../controllers/authController";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    // fastify.get("/twitch", {
    //     handler: handleTwitchAuth,
    // });

    fastify.get("/twitch/callback", {
        handler: (req, reply) => handleTwitchCallback(fastify, req, reply),
    });

    fastify.get("/check-session", checkSession);

    fastify.get("/user", getUser);

    fastify.get("/refresh", refreshToken);
    done();
}
