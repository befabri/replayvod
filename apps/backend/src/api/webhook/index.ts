import { PrismaClient } from "@prisma/client/extension";
import { ChannelRepository } from "../channel/channel.repository";
import { DownloadService } from "../download/download.service";
import { ScheduleRepository } from "../schedule/schedule.repository";
import { WebhookHandler } from "./webhook.handler";
import { WebhookService } from "./webhook.service";

export type WebhookModule = {
    service: WebhookService;
    handler: WebhookHandler;
};

export const webhookModule = (
    db: PrismaClient,
    channelRepository: ChannelRepository,
    downloadService: DownloadService,
    scheduleRepository: ScheduleRepository
): WebhookModule => {
    const service = new WebhookService(db, channelRepository, downloadService, scheduleRepository);
    const handler = new WebhookHandler();

    return {
        service,
        handler,
    };
};
