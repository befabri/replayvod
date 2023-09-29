import fastify from "fastify";
import path from "path";
import dotenv from "dotenv";

dotenv.config({ path: path.resolve(__dirname, "../.env") });

let app;
let ROOT_DIR;

if (process.env.NODE_ENV === "production") {
    ROOT_DIR = __dirname;
} else {
    ROOT_DIR = path.join(__dirname, "..");
}

if (process.env.NODE_ENV === "dev") {
    app = fastify({
        logger: {
            level: "info",
            timestamp: () => `,"time":"${new Date().toISOString()}"`,
        },
    });
} else if (process.env.NODE_ENV === "production") {
    app = fastify({
        logger: {
            level: "info",
            timestamp: () => `,"time":"${new Date().toISOString()}"`,
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
