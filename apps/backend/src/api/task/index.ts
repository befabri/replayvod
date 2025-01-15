import { PrismaClient } from "@prisma/client/extension";
import { CategoryRepository } from "../category/category.repository";
import { EventSubService } from "../event-sub/eventSub.service";
import { VideoRepository } from "../video/video.repository";
import { TaskHandler } from "./task.handler";
import { TaskRepository } from "./task.repository";

export type TaskModule = {
    repository: TaskRepository;
    handler: TaskHandler;
};

export const taskModule = (
    db: PrismaClient,
    categoryRepository: CategoryRepository,
    eventSubService: EventSubService,
    videoRepository: VideoRepository
): TaskModule => {
    const repository = new TaskRepository(db, categoryRepository, eventSubService, videoRepository);
    const handler = new TaskHandler();

    return {
        repository,
        handler,
    };
};
