import { MongoClient, Collection } from "mongodb";
import { getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import UserService from "./userService";
import youtubedl from "youtube-dl-exec";
import { Stream } from "../models/twitchModel";
import { Video } from "../models/videoModel";
import { DownloadSchedule } from "../models/downloadModel";

class ScheduleService {
  twitchAPI: TwitchAPI;

  constructor() {
    this.twitchAPI = new TwitchAPI();
  }

  async insertIntoDb(data: DownloadSchedule) {
    const db = await getDbInstance();
    const scheduleCollection = db.collection("schedule");
    await scheduleCollection.insertOne(data);
  }
}

export default ScheduleService;
