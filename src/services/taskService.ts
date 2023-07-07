import { getDbInstance } from "../models/db";
import { DownloadSchedule } from "../models/downloadModel";
import { Task } from "../models/Task";
import VideoService from "./videoService";
import EventSubService from "./eventSubService";
import { logger } from "../middlewares/loggerMiddleware";

class TaskService {
    private videoService = new VideoService();
    private eventSubService = new EventSubService();

    async insertIntoDb(data: DownloadSchedule) {
        const db = await getDbInstance();
        const scheduleCollection = db.collection("schedule");
        await scheduleCollection.insertOne(data);
    }

    async getTask(id: string) {
        const db = await getDbInstance();
        const taskCollection = db.collection("tasks");
        return taskCollection.findOne({ id: id });
    }

    async getAllTasks() {
        const db = await getDbInstance();
        const taskCollection = db.collection("tasks");
        return taskCollection.find().toArray();
    }

    private taskRunners: { [taskType: string]: (taskMetadata?: any) => Promise<any> } = {
        generateMissingThumbnail: (taskMetadata?: any) => this.videoService.generateMissingThumbnailsAndUpdate(),
        fixMalformedVideos: (taskMetadata?: any) => this.videoService.fixMalformedVideos(),
        subToAllStreamEventFollowed: (taskMetadata?: any) => this.eventSubService.subToAllStreamEventFollowed(),
    };

    async runTask(id: string) {
        const db = await getDbInstance();
        const taskCollection = db.collection("tasks");
        const task = await taskCollection.findOne({ id: id });
        if (!task) {
            logger.error(`Task not found: ${id}`);
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
    }

    async updateTaskExecution(id: string, startTime: number, executionDuration: number, interval: number) {
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
    }
}

export default TaskService;
