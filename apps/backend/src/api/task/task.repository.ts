import { PrismaClient } from "@prisma/client";
import { logger as rootLogger } from "../../app";
import { CategoryRepository } from "../category/category.repository";
import { EventSubService } from "../event-sub/eventSub.service";
import { VideoRepository } from "../video/video.repository";
const logger = rootLogger.child({ domain: "task", service: "repository" });

export class TaskRepository {
    constructor(
        private db: PrismaClient,
        private categoryRepository: CategoryRepository,
        private eventSubService: EventSubService,
        private videoRepository: VideoRepository
    ) {}

    getTask = async (id: string) => {
        return this.db.task.findUnique({ where: { id: id } });
    };

    getAllTasks = async () => {
        return this.db.task.findMany();
    };

    private taskRunners: { [taskType: string]: (taskMetadata?: any) => Promise<any> } = {
        generateMissingThumbnail: (_taskMetadata?: any) =>
            this.videoRepository.generateMissingThumbnailsAndUpdate(),
        fixMalformedVideos: (_taskMetadata?: any) => this.videoRepository.fixMalformedVideos(),
        subToAllStreamEventFollowed: (_taskMetadata?: any) => this.eventSubService.subToAllChannelFollowed(),
        updateMissingBoxArtUrls: (_taskMetadata?: any) => this.categoryRepository.updateMissingBoxArtUrls(),
    };

    runTask = async (id: string) => {
        logger.info("Running task...");
        const task = await this.db.task.findUnique({ where: { id: id } });
        if (!task) {
            logger.error({ taskId: id }, "Task not found");
            throw new Error(`Task not found: ${id}`);
        }
        const taskRunner = this.taskRunners[task.taskType];
        if (!taskRunner) {
            logger.error(`Unrecognized task type: ${task.taskType}`);
            throw new Error(`Unrecognized task type: ${task.taskType}`);
        }
        const startTime = Date.now();
        await taskRunner(task.metadata);
        const endTime = Date.now();
        const executionDuration = endTime - startTime;
        const updatedTask = await this.updateTaskExecution(id, startTime, executionDuration, task.interval);
        return updatedTask;
    };

    private updateTaskExecution = async (
        id: string,
        startTime: number,
        executionDuration: number,
        interval: number
    ) => {
        await this.db.task.update({
            where: { id: id },
            data: {
                lastExecution: new Date(startTime),
                lastDuration: executionDuration,
                nextExecution: new Date(startTime + interval),
            },
        });
        return this.db.task.findUnique({ where: { id: id } });
    };
}
