import fastify from "fastify";

const app = fastify({
    logger: {
        level: "info",
        timestamp: () => `,"time":"${new Date().toISOString()}"`,
    },
    // logger: {
    //     transport: {
    //         target: path.resolve(__dirname, "./file-transport.js"),
    //     },
    // },
});

export default app;
export const logger = app.log;

logger.info("I can even log here, before the app is running.");
