import dotenv from "dotenv";
import path from "path";
import cors from "@fastify/cors";
import server, { logger } from "./app";
import { Prisma, PrismaClient } from "@prisma/client";
import fastifyPassport from "@fastify/passport";
import fastifySecureSession from "@fastify/secure-session";
import fastifyCookie from "@fastify/cookie";
import routes from "./routes";
import moment from "moment-timezone";
import "./middlewares/passport";

dotenv.config({ path: path.resolve(__dirname, "../.env") });

const PORT: number = 8080;
const HOST: string = "0.0.0.0";
const SESSION_SECRET = process.env.SESSION_SECRET;
const REACT_URL = process.env.REACT_URL;
moment.tz.setDefault("Europe/Paris");

if (!SESSION_SECRET) {
    console.error("No session secret provided. Shutting down...");
    process.exit(1);
}

export const prisma = new PrismaClient();

server.register(cors, {
    origin: REACT_URL,
    credentials: true,
});

server.register(fastifyCookie);

server.register(fastifySecureSession, {
    key: Buffer.from(SESSION_SECRET), // key: fs.readFileSync(path.join(__dirname, '../secret-key')),
    cookie: { httpOnly: true },
});

server.register(fastifyPassport.initialize());
server.register(fastifyPassport.secureSession());

server.register(routes, { prefix: "/api" });

const start = async () => {
    try {
        await server.listen(PORT, HOST);
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
