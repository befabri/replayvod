import { getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import { DownloadSchedule } from "../models/downloadModel";
import { Task } from "../models/Task";
import VideoService from "./videoService";

class ScheduleService {
  private videoService = new VideoService();
  twitchAPI: TwitchAPI;

  constructor() {
    this.twitchAPI = new TwitchAPI();
  }

  async insertIntoDb(data: DownloadSchedule) {
    const db = await getDbInstance();
    const scheduleCollection = db.collection("schedule");
    await scheduleCollection.insertOne(data);
  }

  async getTask(id: string) {
    const db = await getDbInstance();
    const taskCollection = db.collection("task");
    return taskCollection.findOne({ id: id });
  }

  async getAllTasks() {
    const db = await getDbInstance();
    const taskCollection = db.collection("task");
    return taskCollection.find().toArray();
  }

  private taskRunners: { [taskType: string]: (taskMetadata?: any) => Promise<any> } = {
    generateMissingThumbnail: (taskMetadata?: any) => this.videoService.generateMissingThumbnailsAndUpdate(),
  };

  async runTask(id: string) {
    const db = await getDbInstance();
    const taskCollection = db.collection("task");
    const task = await taskCollection.findOne({ id: id });
    if (!task) {
      throw new Error(`Task not found: ${id}`);
    }
    const taskRunner = this.taskRunners[task.taskType];
    if (!taskRunner) {
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
    const taskCollection = db.collection("task");
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

export default ScheduleService;
