import { getDbInstance } from "../models/db";
import { Task } from "../models/Task";
import { videoService, eventSubService } from "../services";
import { logger } from "../middlewares/loggerMiddleware";

export const getTask = async (id: string) => {
    const db = await getDbInstance();
    const taskCollection = db.collection("tasks");
    return taskCollection.findOne({ id: id });
};

export const getAllTasks = async () => {
    const db = await getDbInstance();
    const taskCollection = db.collection("tasks");
    return taskCollection.find().toArray();
};

const taskRunners: { [taskType: string]: (taskMetadata?: any) => Promise<any> } = {
    generateMissingThumbnail: (taskMetadata?: any) => videoService.generateMissingThumbnailsAndUpdate(),
    fixMalformedVideos: (taskMetadata?: any) => videoService.fixMalformedVideos(),
    subToAllStreamEventFollowed: (taskMetadata?: any) => eventSubService.subToAllStreamEventFollowed(),
};

export const runTask = async (id: string) => {
    const db = await getDbInstance();
    const taskCollection = db.collection("tasks");
    const task = await taskCollection.findOne({ id: id });
    if (!task) {
        logger.error(`Task not found: ${id}`);
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
    const db = await getDbInstance();
    const taskCollection = db.collection("tasks");
    await taskCollection.updateOne(
        { id: id },
        {
            $set: {
                lastExecution: new Date(startTime),
                lastDuration: executionDuration,
                nextExecution: new Date(startTime + interval),
            },
        }
    );
    return taskCollection.findOne({ id: id });
};

export default {
    getTask,
    getAllTasks,
    runTask,
    updateTaskExecution,
};
