import { FastifyInstance } from "fastify";
import { isUserWhitelisted, userAuthenticated } from "../../middlewares/middleware.auth";

export default function (fastify: FastifyInstance, _opts: any, done: any) {
    const handler = fastify.auth.handler;

    fastify.get("/twitch/callback", {
        handler: (req, reply) => handler.handleTwitchCallback(fastify, req, reply),
    });

    fastify.get("/check-session", handler.checkSession);

    fastify.get("/user", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: handler.getUser,
    });

    fastify.get("/refresh", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: handler.refreshToken,
    });

    fastify.post("/signout", {
        preHandler: [isUserWhitelisted, userAuthenticated],
        handler: handler.signOut,
    });

    done();
}
