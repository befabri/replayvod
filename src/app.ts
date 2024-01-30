import fastify, { FastifyInstance } from "fastify";
import path from "path";
import { EnvType, envSchema } from "./utils/env";
import { ZodError } from "zod";
import { DateTime } from "luxon";

let app: FastifyInstance;
let ROOT_DIR: string;
let env: EnvType;

try {
    env = envSchema.parse(process.env);
} catch (error) {
    if (error instanceof ZodError) {
        console.error("Environment variable validation failed: ", error.message);
    } else {
        console.error("There is a problem with the environment variable");
    }
    process.exit(1);
}

if (env.nodeEnv === "production") {
    ROOT_DIR = __dirname;
} else {
    ROOT_DIR = path.join(__dirname, "..");
}

const formatDateTime = () => DateTime.now().setZone("Europe/Paris").toISO();

app = fastify({
    logger: {
        level: "info",
        timestamp: () => `,"time":"${formatDateTime()}"`,
    },
});

export default app;
export { env };
export const logger = app.log;
logger.info("Loading...");
