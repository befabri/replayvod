import fastify, { FastifyInstance } from "fastify";
import { EnvType, envSchema } from "./utils/env";
import { ZodError } from "zod";
import { DateTime } from "luxon";

let app: FastifyInstance;
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

const formatDateTime = () => DateTime.now().setZone('Europe/Paris').toFormat('dd-MM-yyyy HH:mm:ss');

app = fastify({
    logger: {
        level: "info",
        timestamp: () => `,"time":"${formatDateTime()}"`,
        base: null,
    },
});

export default app;
export { env };
export const logger = app.log;
logger.info("Loading...");
