import { getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import UserService from "./userService";
import youtubedl from "youtube-dl-exec";
import { Stream } from "../models/twitchModel";
import { Video } from "../models/videoModel";
const path = require("path");

const userService = new UserService();

class downloadService {
  twitchAPI: TwitchAPI;

  constructor() {
    this.twitchAPI = new TwitchAPI();
  }

  async planningRecord(userId: string) {
    const user = await userService.getUserDetailDB(userId);
    return "Successful registration planning";
  }

  async saveVideoInfo(
    userRequesting: string,
    broadcasterId: string,
    displayName: string,
    videoName: string,
    startAt: Date,
    status: string,
    jobId: string,
    stream: Stream
  ) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const gamesCollection = db.collection("games");

    const gameData = await gamesCollection.findOne({ id: stream.game_id });
    let gameDetail = [{ id: stream.game_id, name: "" }];

    if (gameData) {
      gameDetail[0].name = gameData.name;
    }

    const videoData: Video = {
      id: stream.id,
      filename: videoName,
      status: status,
      display_name: displayName,
      broadcaster_id: broadcasterId,
      requested_by: userRequesting,
      start_download_at: startAt,
      downloaded_at: "",
      job_id: jobId,
      category: gameDetail,
      title: [stream.title],
      tags: stream.tags,
      viewer_count: stream.viewer_count,
      language: stream.language,
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
    jobId: string,
    stream: Stream
  ) {
    const startAt = new Date();
    await this.saveVideoInfo(
      userRequesting,
      broadcasterId,
      displayName,
      path.basename(videoPath),
      startAt,
      "Pending",
      jobId,
      stream
    );
    await youtubedl.exec(`https://www.twitch.tv/${login}`, {
      output: videoPath,
      cookies: cookiesFilePath,
    });
    return videoPath;
  }

  async finishDownload(videoPath: string) {
    const endAt = new Date();
    await this.updateVideoInfo(path.basename(videoPath), endAt, "Finished");
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

  async updateVideoCollection(user_id: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    const stream = await this.twitchAPI.getStreamByUserId(user_id);
    const videoData = await videoCollection.findOne({ broadcaster_id: user_id });

    if (videoData) {
      if (!videoData.category.some((category: { id: string; name: string }) => category.id === stream.game_id)) {
        videoData.category.push({ id: stream.game_id, name: stream.game_name });
      }

      if (!videoData.title.includes(stream.title)) {
        videoData.title.push(stream.title);
      }

      if (!videoData.tags.some((tag: string) => stream.tags.includes(tag))) {
        videoData.tags.push(...stream.tags);
      }

      if (stream.viewer_count > videoData.viewer_count) {
        videoData.viewer_count = stream.viewer_count;
      }

      return videoCollection.updateOne({ broadcaster_id: user_id }, { $set: videoData });
    } else {
      throw new Error("No video data found for the provided user_id.");
    }
  }
}

export default downloadService;
