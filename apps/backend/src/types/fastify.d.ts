import { AuthModule } from "../api/auth";
import { CategoryModule } from "../api/category";
import { ChannelModule } from "../api/channel";
import { DownloadModule } from "../api/download";
import { LoggingModule } from "../api/logging";
import { ScheduleModule } from "../api/schedule";
import { SettingsModule } from "../api/settings";
import { TaskModule } from "../api/task";
import { UserModule } from "../api/user";
import { VideoModule } from "../api/video";
import { WebhookModule } from "../api/webhook";
import { EventSubModule } from "../api/event-sub";
import { Prisma } from "@prisma/client";

declare module "fastify" {
    interface FastifyInstance {
        auth: AuthModule;
        category: CategoryModule;
        channel: ChannelModule;
        download: DownloadModule;
        eventSub: EventSubModule;
        logging: LoggingModule;
        schedule: ScheduleModule;
        settings: SettingsModule;
        task: TaskModule;
        user: UserModule;
        video: VideoModule;
        webhook: WebhookModule;
        prisma: Prisma;
    }
}
