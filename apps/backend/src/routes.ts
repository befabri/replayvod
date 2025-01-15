import { FastifyInstance, FastifyPluginAsync } from "fastify";
import authRoutes from "./api/auth/auth.route";
import userRoutes from "./api/user/user.route";
import channelRoutes from "./api/channel/channel.route";
import downloadRoutes from "./api/download/download.route";
import videoRoutes from "./api/video/video.route";
import taskRoutes from "./api/task/task.route";
import logRoutes from "./api/logging/logging.route";
import webhookRoutes from "./api/webhook/webhook.route";
import categoryRoutes from "./api/category/category.route";
import settingsRoutes from "./api/settings/settings.route";
import scheduleRoutes from "./api/schedule/schedule.route";
import eventSubRoutes from "./api/event-sub/eventSub.route";

const routes: FastifyPluginAsync = async (server: FastifyInstance) => {
    server.register(authRoutes, { prefix: "/auth" });
    server.register(userRoutes, { prefix: "/user" });
    server.register(channelRoutes, { prefix: "/channel" });
    server.register(downloadRoutes, { prefix: "/download" });
    server.register(scheduleRoutes, { prefix: "/schedule" });
    server.register(videoRoutes, { prefix: "/video" });
    server.register(eventSubRoutes, { prefix: "/event-sub" });
    server.register(taskRoutes, { prefix: "/task" });
    server.register(logRoutes, { prefix: "/log" });
    server.register(webhookRoutes, { prefix: "/webhook" });
    server.register(categoryRoutes, { prefix: "/category" });
    server.register(settingsRoutes, { prefix: "/settings" });
};

export default routes;
