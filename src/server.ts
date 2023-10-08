import dotenv from "dotenv";
import path from "path";
import cors from "@fastify/cors";
import server, { logger } from "./app";
import { Prisma, PrismaClient } from "@prisma/client";
import fastifySecureSession from "@fastify/secure-session";
import fastifyCookie from "@fastify/cookie";
import routes from "./routes";
import moment from "moment-timezone";
import fastifyStatic from "@fastify/static";
import { isUserWhitelisted, userAuthenticated } from "./middlewares/authMiddleware";
import { videoService } from "./api/video";
const oauthPlugin = require("@fastify/oauth2");
const fs = require("fs");

dotenv.config({ path: path.resolve(__dirname, "../.env") });

const PORT: number = 8080;
const HOST: string = "0.0.0.0";
const REACT_URL = process.env.REACT_URL;
const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;
const TWITCH_SECRET = process.env.TWITCH_SECRET;
const CALLBACK_URL = process.env.CALLBACK_URL;
moment.tz.setDefault("Europe/Paris");

logger.info("Launching Fastify in %s environment", process.env.NODE_ENV);

export const prisma = new PrismaClient();

let ROOT_DIR = "";
if (process.env.NODE_ENV === "production") {
    ROOT_DIR = __dirname;
} else {
    ROOT_DIR = path.join(__dirname, "..");
}

server.register(cors, {
    origin: REACT_URL,
    credentials: true,
});

server.register(async (instance, opts) => {
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
    key: fs.readFileSync(path.join(ROOT_DIR, "secret", "secret-key")),
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
            id: TWITCH_CLIENT_ID,
            secret: TWITCH_SECRET,
        },
        auth: oauthPlugin.TWITCH_CONFIGURATION,
    },
    tokenRequestParams: {
        client_id: TWITCH_CLIENT_ID,
        client_secret: TWITCH_SECRET,
    },
    startRedirectPath: "/api/auth/twitch",
    callbackUri: CALLBACK_URL,
    scope: ["user:read:email", "user:read:follows"],
});

server.register(routes, { prefix: "/api" });

const start = async () => {
    logger.info("Starting Fastify server...");
    try {
        await videoService.setVideoFailed();
        await server.listen({ port: PORT, host: HOST });
        const address = server.server.address();
        const port = typeof address === "string" ? address : address?.port;
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
