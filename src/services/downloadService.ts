import { getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import { v4 as uuidv4 } from "uuid";
import UserService from "./userService";
import youtubedl from "youtube-dl-exec";

const userService = new UserService();

class downloadService {
  twitchAPI: TwitchAPI;

  constructor() {
    this.twitchAPI = new TwitchAPI();
  }

  async planningRecord(userId: string) {
    const user = await userService.getUserDetailDB(userId);
    console.log(user);
    return "Successful registration planning";
  }

  async saveVideoInfo(
    userRequesting: string,
    broadcasterId: string,
    displayName: string,
    videoName: string,
    startAt: Date,
    status: string,
    jobId: string
  ) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    const videoData = {
      filename: videoName,
      status: status,
      display_name: displayName,
      broadcaster_id: broadcasterId,
      requested_by: userRequesting,
      start_download_at: startAt,
      downloaded_at: "",
      job_id: jobId,
    };

    return videoCollection.insertOne(videoData);
  }

  async updateVideoInfo(videoName: string, endAt: Date, status: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    return videoCollection.updateOne(
      { filename: videoName },
      {
        $set: {
          downloaded_at: endAt,
          status: status,
        },
      }
    );
  }

  async startDownload(
    userRequesting: string,
    broadcasterId: string,
    displayName: string,
    login: string,
    videoPath: string,
    cookiesFilePath: string,
    jobId: string
  ) {
    const startAt = new Date();
    await this.saveVideoInfo(userRequesting, broadcasterId, displayName, videoPath, startAt, "Pending", jobId);
    await youtubedl.exec(`https://www.twitch.tv/${login}`, {
      output: videoPath,
      cookies: cookiesFilePath,
    });
    return videoPath;
  }

  async finishDownload(videoPath: string) {
    const endAt = new Date();
    await this.updateVideoInfo(videoPath, endAt, "Finished");
  }

  async findPendingJob(broadcaster_id: string) {
    const db = await getDbInstance();
    const jobCollection = db.collection("videos");

    return jobCollection.findOne({ broadcaster_id, status: "Pending" });
  }

  static async setVideoFailed(jobId: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    return videoCollection.updateOne(
      { job_id: jobId },
      {
        $set: {
          status: "Failed",
        },
      }
    );
  }
}

export default downloadService;
