import { PrismaClient } from "@prisma/client/extension";
import { CacheService } from "../../services/service.cache";
import { TagService } from "../../services/service.tag";
import { TitleService } from "../../services/service.title";
import { TwitchService } from "../../services/service.twitch";
import { CategoryRepository } from "../category/category.repository";
import { ChannelHandler } from "./channel.handler";
import { ChannelRepository } from "./channel.repository";

export type ChannelModule = {
    repository: ChannelRepository;
    handler: ChannelHandler;
};

export const channelModule = (
    db: PrismaClient,
    categoryRepository: CategoryRepository,
    cacheService: CacheService,
    titleService: TitleService,
    tagService: TagService,
    twitchService: TwitchService
): ChannelModule => {
    const repository = new ChannelRepository(
        db,
        categoryRepository,
        cacheService,
        titleService,
        tagService,
        twitchService
    );
    const handler = new ChannelHandler();

    return {
        repository,
        handler,
    };
};
