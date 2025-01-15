import path from "path";
import cors from "@fastify/cors";
import app, { env, logger } from "./app";
import fastifySecureSession from "@fastify/secure-session";
import fastifyCookie from "@fastify/cookie";
import fs from "fs";
import routes from "./routes";
import fastifyStatic from "@fastify/static";
import { isUserWhitelisted, userAuthenticated } from "./middlewares/middleware.auth";
import oauthPlugin from "@fastify/oauth2";
import { readFileSync } from "fs";
import { TWITCH_ENDPOINT } from "./models/model.twitch";
import { SECRET_FILENAME, SECRET_PATH, THUMBNAIL_PATH, VIDEO_PATH } from "./constants/constant.folder";
import { HOST, PORT } from "./constants/constant.server";
import { modulesPlugin } from "./plugins/plugin.module";
import { prismaPlugin } from "./plugins/plugin.prisma";

const server = app;

logger.info(`Launching Fastify in '${env.nodeEnv}' environment`);

if (!fs.existsSync(THUMBNAIL_PATH)) {
    logger.error("THUMBNAIL folder doesn't exist, creating...");
    fs.mkdirSync(THUMBNAIL_PATH, { recursive: false });
}

if (!fs.existsSync(VIDEO_PATH)) {
    logger.error("VIDEO folder doesn't exist, creating...");
    fs.mkdirSync(VIDEO_PATH, { recursive: false });
}

server.register(cors, {
    origin: env.reactUrl,
    credentials: true,
});

server.register(async (instance, _opts) => {
    instance.register(fastifyStatic, {
        root: THUMBNAIL_PATH,
        prefix: "/api/video/thumbnail/",
        serve: true,
    });

    instance.addHook("preHandler", isUserWhitelisted);
    instance.addHook("preHandler", userAuthenticated);
});

server.register(fastifyCookie);

server.register(prismaPlugin);

server.register(fastifySecureSession, {
    key: readFileSync(path.resolve(SECRET_PATH, SECRET_FILENAME)),
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

server.register(modulesPlugin);

server.register(routes, { prefix: "/api" });

server.get("/", (_request, reply) => {
    reply.code(444).send();
});

server.addHook("onReady", async () => {
    await server.video.repository.setVideoFailed();
});

const start = async () => {
    logger.info("Starting Fastify server...");
    try {
        await server.listen({ port: PORT, host: HOST });
    } catch (err) {
        logger.error(err);
        process.exit(1);
    }
};

process.on("SIGINT", async () => {
    logger.info("Closing SIGINT...");
    process.exit();
});

process.on("SIGTERM", async () => {
    logger.info("Closing SIGTERM...");
});

start();
