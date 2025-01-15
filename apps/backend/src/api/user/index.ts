import { PrismaClient } from "@prisma/client/extension";
import { CacheService } from "../../services/service.cache";
import { TwitchService } from "../../services/service.twitch";
import { ChannelRepository } from "../channel/channel.repository";
import { UserHandler } from "./user.handler";
import { UserRepository } from "./user.repository";

export type UserModule = {
    repository: UserRepository;
    handler: UserHandler;
};

export const userModule = (
    db: PrismaClient,
    channelRepository: ChannelRepository,
    twitchService: TwitchService,
    cacheService: CacheService
): UserModule => {
    const repository = new UserRepository(db, channelRepository, twitchService, cacheService);
    const handler = new UserHandler();

    return {
        repository,
        handler,
    };
};
