import { PrismaClient } from "@prisma/client/extension";
import { TagService } from "../../services/service.tag";
import { TitleService } from "../../services/service.title";
import { CategoryRepository } from "../category/category.repository";
import { VideoHandler } from "./video.handler";
import { VideoRepository } from "./video.repository";

export type VideoModule = {
    repository: VideoRepository;
    handler: VideoHandler;
};

export const videoModule = (
    db: PrismaClient,
    categoryRepository: CategoryRepository,
    tagService: TagService,
    titleService: TitleService
): VideoModule => {
    const repository = new VideoRepository(db, categoryRepository, tagService, titleService);
    const handler = new VideoHandler();

    return {
        repository,
        handler,
    };
};
