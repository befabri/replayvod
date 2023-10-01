import { FastifyInstance, FastifyPluginAsync } from "fastify";
import authRoutes from "./authRoutes";
import userRoutes from "./userRoutes";
import downloadRoutes from "./downloadRoutes";
import videoRoutes from "./videoRoutes";
import manageRoutes from "./manageRoutes";
import taskRoutes from "./taskRoutes";
import logRoutes from "./logRoutes";
import webhookRoutes from "./webhookRoutes";
import categoryRoutes from "./categoryRoutes";

const routes: FastifyPluginAsync = async (server: FastifyInstance) => {
    server.register(authRoutes, { prefix: "/auth" });
    server.register(userRoutes, { prefix: "/users" });
    server.register(downloadRoutes, { prefix: "/dl" });
    server.register(videoRoutes, { prefix: "/videos" });
    server.register(manageRoutes, { prefix: "/twitch" });
    server.register(taskRoutes, { prefix: "/task" });
    server.register(logRoutes, { prefix: "/log" });
    server.register(webhookRoutes, { prefix: "/webhook" });
    server.register(categoryRoutes, { prefix: "/category" });
};

export default routes;
