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

  private taskRunners: { [taskType: string]: (...args: any[]) => Promise<any> } = {
    generateMissingThumbnail: this.videoService.generateMissingThumbnailsAndUpdate,
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

    return await taskRunner(task.metadata);
  }
}

export default ScheduleService;
