import { PrismaClient } from "@prisma/client/extension";
import { CategoryRepository } from "../category/category.repository";
import { ChannelRepository } from "../channel/channel.repository";
import { VideoRepository } from "../video/video.repository";
import { ScheduleDTO } from "./schedule.dto";
import { ScheduleHandler } from "./schedule.handler";
import { ScheduleRepository } from "./schedule.repository";

export type ScheduleModule = {
    repository: ScheduleRepository;
    handler: ScheduleHandler;
    dto: ScheduleDTO;
};

export const scheduleModule = (
    db: PrismaClient,
    videoRepository: VideoRepository,
    channelRepository: ChannelRepository,
    categoryRepository: CategoryRepository
): ScheduleModule => {
    const dto = new ScheduleDTO(channelRepository, videoRepository, categoryRepository);
    const repository = new ScheduleRepository(db, videoRepository, dto);
    const handler = new ScheduleHandler();

    return {
        repository,
        handler,
        dto,
    };
};
