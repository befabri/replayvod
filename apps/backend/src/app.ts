import fastify, { FastifyInstance, FastifyServerOptions } from "fastify";
import { EnvType, envSchema } from "./utils/env";
import { ZodError } from "zod";
import { formatTimestamp } from "./utils/utils";

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

const options: FastifyServerOptions = {
    logger: {
        level: "info",
        timestamp: formatTimestamp,
        base: null,
    },
};

app = fastify(options);

export default app;
export { env };
export const logger = app.log;
logger.info("Loading...");
