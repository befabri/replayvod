import path from "path";
import cors from "@fastify/cors";
import app, { env, logger } from "./app";
import { PrismaClient } from "@prisma/client";
import fastifySecureSession from "@fastify/secure-session";
import fastifyCookie from "@fastify/cookie";
import routes from "./routes";
import fastifyStatic from "@fastify/static";
import { isUserWhitelisted, userAuthenticated } from "./middlewares/authMiddleware";
import { videoFeature } from "./api/video";
import { TWITCH_ENDPOINT } from "./constants/twitchConstants";
import oauthPlugin from "@fastify/oauth2";
import { readFileSync } from "fs";

const PORT: number = 8080;
const HOST: string = "0.0.0.0";
const server = app;

logger.info("Launching Fastify in %s environment", env.nodeEnv);
export const prisma = new PrismaClient();

let ROOT_DIR = "";
if (env.nodeEnv === "production") {
    ROOT_DIR = __dirname;
} else {
    ROOT_DIR = path.join(__dirname, "..");
}

server.register(cors, {
    origin: env.reactUrl,
    credentials: true,
});

server.register(async (instance, _opts) => {
    instance.register(fastifyStatic, {
        root: path.join(ROOT_DIR, "public", "thumbnail"),
        prefix: "/api/video/thumbnail/",
        serve: true,
    });

    instance.addHook("preHandler", isUserWhitelisted);
    instance.addHook("preHandler", userAuthenticated);
});

server.register(fastifyCookie);

server.register(fastifySecureSession, {
    key: readFileSync(path.join(ROOT_DIR, "secret", "secret-key")),
    cookieName: "session",
    cookie: {
        path: "/",
        httpOnly: true,
        secure: true,
    },
});

server.register(oauthPlugin, {
    name: "twitchOauth2",
    credentials: {
        client: {
            id: env.twitchClientId,
            secret: env.twitchSecret,
        },
        auth: oauthPlugin.TWITCH_CONFIGURATION,
    },
    tokenRequestParams: {
        client_id: env.twitchClientId,
        client_secret: env.twitchSecret,
    },
    startRedirectPath: TWITCH_ENDPOINT,
    callbackUri: env.callbackUrl,
    scope: ["user:read:email", "user:read:follows"],
});

server.register(routes, { prefix: "/api" });

server.get("/", (_request, reply) => {
    reply.code(444).send();
});

const start = async () => {
    logger.info("Starting Fastify server...");
    try {
        await videoFeature.setVideoFailed();
        await server.listen({ port: PORT, host: HOST });
    } catch (err) {
        logger.error(err);
        process.exit(1);
    }
};

process.on("SIGINT", async () => {
    console.log("Closing Prisma Client...");
    await prisma.$disconnect();
    process.exit();
});

process.on("SIGTERM", async () => {
    console.log("Closing Prisma Client...");
    await prisma.$disconnect();
});

start();
