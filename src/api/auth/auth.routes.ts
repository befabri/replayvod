import { FastifyInstance } from "fastify";
import * as authHandler from "./auth.handlers";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    // fastify.get("/twitch", {
    //     handler: handleTwitchAuth,
    // });

    fastify.get("/twitch/callback", {
        handler: (req, reply) => authHandler.handleTwitchCallback(fastify, req, reply),
    });

    fastify.get("/check-session", authHandler.checkSession);

    fastify.get("/user", authHandler.getUser);

    fastify.get("/refresh", authHandler.refreshToken);

    fastify.post("/signout", authHandler.signOut);
    done();
}
