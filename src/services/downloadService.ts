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
    status: string
  ) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    const videoData = {
      filename: videoName,
      start_download_at: startAt,
      status: status,
      requested_by: userRequesting,
      broadcaster_id: broadcasterId,
      display_name: displayName,
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

  async startDownload(userRequesting: string, broadcasterId: string, displayName: string, videoName: string, cookiesFilePath: string) {
    const startAt = new Date();
    await this.saveVideoInfo(userRequesting, broadcasterId, displayName, videoName, startAt, "Pending");
    await youtubedl.exec(`https://www.twitch.tv/${broadcasterId}`, {
      output: videoName,
      cookies: cookiesFilePath,
    });
    return videoName;
  }
  
  async finishDownload(videoName: string) {
    const endAt = new Date();
    await this.updateVideoInfo(videoName, endAt, "Finished");
  }
}
}

export default downloadService;
