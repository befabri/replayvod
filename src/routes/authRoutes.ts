import { FastifyInstance, FastifyPluginAsync } from "fastify";
import {
    handleTwitchAuth,
    handleTwitchCallback,
    checkSession,
    getUser,
    refreshToken,
} from "../controllers/authController";
import dotenv from "dotenv";
import fastifyPassport from "@fastify/passport";

dotenv.config();

const REDIRECT_URL = process.env.REDIRECT_URL || "/";

export default function (fastify: FastifyInstance, opts: any, done: any) {
    fastify.get(
        "/twitch",
        {
            preHandler: fastifyPassport.authenticate("twitch", {
                scope: ["user:read:email", "user:read:follows"],
            }),
        },
        handleTwitchAuth
    );

    fastify.get(
        "/twitch/callback",
        {
            preHandler: fastifyPassport.authenticate("twitch", {
                successRedirect: REDIRECT_URL,
                failureRedirect: "https://google.com",
            }),
        },
        handleTwitchCallback
    );

    fastify.get("/check-session", checkSession);

    fastify.get("/user", getUser);

    fastify.get("/refresh", refreshToken);
    done();
}
