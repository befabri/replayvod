import { FastifyInstance, FastifyPluginAsync } from "fastify";
import authRoutes from "./api/auth/auth.routes";
import userRoutes from "./api/user/user.routes";
import channelRoutes from "./api/channel/channel.routes";
import downloadRoutes from "./api/download/download.routes";
import videoRoutes from "./api/video/video.routes";
import twitchRoutes from "./api/twitch/twitch.routes";
import taskRoutes from "./api/task/task.routes";
import logRoutes from "./api/log/log.routes";
import webhookRoutes from "./api/webhook/webhook.routes";
import categoryRoutes from "./api/category/category.routes";

const routes: FastifyPluginAsync = async (server: FastifyInstance) => {
    server.register(authRoutes, { prefix: "/auth" });
    server.register(userRoutes, { prefix: "/user" });
    server.register(channelRoutes, { prefix: "/channel" });
    server.register(downloadRoutes, { prefix: "/download" });
    server.register(videoRoutes, { prefix: "/video" });
    server.register(twitchRoutes, { prefix: "/twitch" });
    server.register(taskRoutes, { prefix: "/task" });
    server.register(logRoutes, { prefix: "/log" });
    server.register(webhookRoutes, { prefix: "/webhook" });
    server.register(categoryRoutes, { prefix: "/category" });
};

export default routes;
