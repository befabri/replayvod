import fastify, { FastifyInstance } from "fastify";
import path from "path";
import moment from "moment-timezone";

let app: FastifyInstance;
let ROOT_DIR: string;

if (process.env.NODE_ENV === "production") {
    ROOT_DIR = __dirname;
} else {
    ROOT_DIR = path.join(__dirname, "..");
}

if (process.env.NODE_ENV === "dev") {
    app = fastify({
        logger: {
            level: "info",
            timestamp: () => `,"time":"${moment().tz("Europe/Paris").format()}"`,
        },
    });
} else if (process.env.NODE_ENV === "production") {
    app = fastify({
        logger: {
            level: "info",
            timestamp: () => `,"time":"${moment().tz("Europe/Paris").format()}"`,
            transport: {
                target: "pino/file",
                options: {
                    destination: path.join(ROOT_DIR, "logs", "replay.log"),
                },
            },
        },
    });
} else {
    throw new Error("Invalid NODE_ENV value. Expected 'dev' or 'production'.");
}

export default app;
export const logger = app.log;
logger.info("Loading...");
