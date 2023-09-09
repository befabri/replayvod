import { Task } from "../models/Task";
import { videoService, eventSubService } from "../services";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "taskService" });

export const getTask = async (id: string) => {
    return prisma.task.findUnique({ where: { id: id } });
};

export const getAllTasks = async () => {
    return prisma.task.findMany();
};

const taskRunners: { [taskType: string]: (taskMetadata?: any) => Promise<any> } = {
    generateMissingThumbnail: (taskMetadata?: any) => videoService.generateMissingThumbnailsAndUpdate(),
    fixMalformedVideos: (taskMetadata?: any) => videoService.fixMalformedVideos(),
    subToAllStreamEventFollowed: (taskMetadata?: any) => eventSubService.subToAllStreamEventFollowed(),
};

export const runTask = async (id: string) => {
    const task = await prisma.task.findUnique({ where: { id: id } });
    if (!task) {
        logger.error({ taskId: id }, "Task not found");
        throw new Error(`Task not found: ${id}`);
    }
    const taskRunner = taskRunners[task.taskType];
    if (!taskRunner) {
        logger.error(`Unrecognized task type: ${task.taskType}`);
        throw new Error(`Unrecognized task type: ${task.taskType}`);
    }
    const startTime = Date.now();
    await taskRunner(task.metadata);
    const endTime = Date.now();
    const executionDuration = endTime - startTime;
    const updatedTask = await updateTaskExecution(id, startTime, executionDuration, task.interval);
    return updatedTask;
};

export const updateTaskExecution = async (
    id: string,
    startTime: number,
    executionDuration: number,
    interval: number
) => {
    await prisma.task.update({
        where: { id: id },
        data: {
            lastExecution: new Date(startTime),
            lastDuration: executionDuration,
            nextExecution: new Date(startTime + interval),
        },
    });
    return prisma.task.findUnique({ where: { id: id } });
};

export default {
    getTask,
    getAllTasks,
    runTask,
    updateTaskExecution,
};
