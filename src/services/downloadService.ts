import { getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import UserService from "./userService";
import { Stream } from "../models/twitchModel";
import { Video } from "../models/videoModel";
import { VideoQuality } from "../models/downloadModel";
import { youtubedlLogger } from "../middlewares/loggerMiddleware";
import VideoService from "./videoService";
import fs from "fs";
import path from "path";
import { duration } from "moment-timezone";
const os = require("os");
const { create: createYoutubeDl } = require("youtube-dl-exec");
let youtubedl;
if (os.platform() === "win32") {
  youtubedl = createYoutubeDl("bin/yt.exe");
} else if (os.platform() === "linux") {
  youtubedl = createYoutubeDl("bin/yt-dlp");
}

class downloadService {
  private userService = new UserService();
  private videoService = new VideoService();

  twitchAPI: TwitchAPI;

  constructor() {
    this.twitchAPI = new TwitchAPI();
  }

  async planningRecord(userId: string) {
    const user = await this.userService.getUserDetailDB(userId);
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
    stream: Stream,
    videoQuality: VideoQuality
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
      quality: videoQuality,
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
    stream: Stream,
    videoQuality: VideoQuality
  ) {
    const startAt = new Date();
    const filename = path.basename(videoPath);
    await this.saveVideoInfo(
      userRequesting,
      broadcasterId,
      displayName,
      filename,
      startAt,
      "Pending",
      jobId,
      stream,
      videoQuality
    );

    return new Promise<string>((resolve, reject) => {
      youtubedlLogger.info(
        `Download: ${JSON.stringify({
          download: `https://www.twitch.tv/${login}`,
          format: `best[height=${videoQuality}]`,
          output: videoPath,
          cookies: cookiesFilePath,
        })} `
      );
      const subprocess = youtubedl.exec(`https://www.twitch.tv/${login}`, {
        format: `best[height=${videoQuality}]`,
        output: videoPath,
      });

      subprocess.stdout.on("data", (chunk) => {
        youtubedlLogger.info(`STDOUT: ${chunk.toString()}`);
      });

      subprocess.stderr.on("data", (chunk) => {
        const message = chunk.toString();
        if (
          message.includes("error") ||
          message.includes("error") ||
          (!message.includes("Skip") && !message.includes("Opening") && !message.includes("frame"))
        ) {
          youtubedlLogger.error(`STDERR: ${message}`);
        } else {
          youtubedlLogger.info(`STDOUT: ${message}`);
        }
      });

      subprocess.on("close", async (code) => {
        if (code !== 0) {
          reject(new Error(`youtube-dl process exited with code ${code}`));
        } else {
          await this.finishDownload(videoPath, filename, login);
          resolve(videoPath);
        }
      });
    });
  }

  async finishDownload(videoPath: string, filename: string, login: string) {
    const endAt = new Date();
    let duration, thumbnailPath, size;

    try {
      duration = await this.videoService.getVideoDuration(videoPath);
    } catch (error) {
      console.error("Error getting video duration:", error);
    }

    try {
      thumbnailPath = await this.videoService.generateSingleThumbnail(videoPath, filename, login);
    } catch (error) {
      console.error("Error generating thumbnail:", error);
    }

    try {
      size = await this.videoService.getVideoSize(videoPath);
    } catch (error) {
      console.error("Error getting video size:", error);
    }

    try {
      await this.videoService.updateVideoData(filename, endAt, thumbnailPath, size, duration);
    } catch (error) {
      console.error("Error updating video data:", error);
    }
  }

  async findPendingJob(broadcaster_id: string) {
    const db = await getDbInstance();
    const jobCollection = db.collection("videos");

    return jobCollection.findOne({ broadcaster_id, status: "Pending" });
  }

  static async setVideoFailed(jobId: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const endAt = new Date();
    return videoCollection.updateOne(
      { job_id: jobId },
      {
        $set: {
          downloaded_at: endAt,
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
