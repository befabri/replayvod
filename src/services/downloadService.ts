import { getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import { v4 as uuidv4 } from "uuid";
import UserService from "./userService";

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

  async saveVideoInfo(userRequesting: string, broadcasterId: string, displayName: string, videoName: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    const videoData = {
      filename: videoName,
      downloaded_at: new Date(),
      requested_by: userRequesting,
      broadcaster_id: broadcasterId,
      display_name: displayName,
    };

    return videoCollection.insertOne(videoData);
  }
}

export default downloadService;
