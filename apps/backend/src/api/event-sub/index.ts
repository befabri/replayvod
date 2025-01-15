import { PrismaClient } from "@prisma/client/extension";
import { CacheService } from "../../services/service.cache";
import { TwitchService } from "../../services/service.twitch";
import { ChannelRepository } from "../channel/channel.repository";
import { UserRepository } from "../user/user.repository";
import { WebhookService } from "../webhook/webhook.service";
import { EventSubHandler } from "./eventSub.handler";
import { EventSubService } from "./eventSub.service";

export type EventSubModule = {
    service: EventSubService;
    handler: EventSubHandler;
};

export const eventSubModule = (
    db: PrismaClient,
    userRepository: UserRepository,
    webhookService: WebhookService,
    channelRepository: ChannelRepository,
    twitchService: TwitchService,
    cacheService: CacheService
): EventSubModule => {
    const service = new EventSubService(
        db,
        userRepository,
        webhookService,
        channelRepository,
        twitchService,
        cacheService
    );
    const handler = new EventSubHandler();

    return {
        service,
        handler,
    };
};
