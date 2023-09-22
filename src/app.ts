import fastify from "fastify";
import path from "path";
import dotenv from "dotenv";

dotenv.config({ path: path.resolve(__dirname, "../.env") });

let app;

if (process.env.NODE_ENV === "dev") {
    app = fastify({
        logger: {
            level: "info",
            timestamp: () => `,"time":"${new Date().toISOString()}"`,
        },
    });
} else if (process.env.NODE_ENV === "prod") {
    app = fastify({
        logger: {
            level: "info",
            timestamp: () => `,"time":"${new Date().toISOString()}"`,
            transport: {
                target: "pino/file",
                options: {
                    destination: path.join(__dirname, "../logs/replay.log"),
                },
            },
        },
    });
} else {
    throw new Error("Invalid NODE_ENV value. Expected 'dev' or 'prod'.");
}

export default app;
export const logger = app.log;

logger.info("I can even log here, before the app is running.");
