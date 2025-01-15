import fp from "fastify-plugin";
import { CacheService } from "../services/service.cache";
import { authModule } from "../api/auth";
import { categoryModule } from "../api/category";
import { channelModule } from "../api/channel";
import { downloadModule } from "../api/download";
import { eventSubModule } from "../api/event-sub";
import { loggingModule } from "../api/logging";
import { scheduleModule } from "../api/schedule";
import { settingsModule } from "../api/settings";
import { taskModule } from "../api/task";
import { userModule } from "../api/user";
import { videoModule } from "../api/video";
import { webhookModule } from "../api/webhook";
import { JobService } from "../services/service.job";
import { TagService } from "../services/service.tag";
import { TitleService } from "../services/service.title";
import { TwitchService } from "../services/service.twitch";
import { FastifyInstance, FastifyPluginOptions } from "fastify";

export const modulesPlugin = fp(
    (fastify: FastifyInstance, _options: FastifyPluginOptions, done) => {
        const db = fastify.prisma;
        const cacheService = new CacheService(db);
        const tagService = new TagService(db);
        const titleService = new TitleService(db);
        const twitchService = new TwitchService(db);
        const jobService = new JobService(db);

        const logging = loggingModule(db);
        const settings = settingsModule(db);
        const category = categoryModule(db, twitchService);
        const channel = channelModule(
            db,
            category.repository,
            cacheService,
            titleService,
            tagService,
            twitchService
        );
        const video = videoModule(db, category.repository, tagService, titleService);
        const download = downloadModule(db, video.repository, jobService);
        const user = userModule(db, channel.repository, twitchService, cacheService);
        const schedule = scheduleModule(db, video.repository, channel.repository, category.repository);
        const webhook = webhookModule(db, channel.repository, download.service, schedule.repository);
        const eventSub = eventSubModule(
            db,
            user.repository,
            webhook.service,
            channel.repository,
            twitchService,
            cacheService
        );

        const task = taskModule(db, category.repository, eventSub.service, video.repository);
        const auth = authModule(user.repository);

        fastify.decorate("auth", auth);
        fastify.decorate("category", category);
        fastify.decorate("channel", channel);
        fastify.decorate("download", download);
        fastify.decorate("eventSub", eventSub);
        fastify.decorate("logging", logging);
        fastify.decorate("schedule", schedule);
        fastify.decorate("settings", settings);
        fastify.decorate("task", task);
        fastify.decorate("user", user);
        fastify.decorate("video", video);
        fastify.decorate("webhook", webhook);
        
        done();
    },
    { name: "fastify-modules" }
);
