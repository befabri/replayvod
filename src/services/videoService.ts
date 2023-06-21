import { getDbInstance } from "../models/db";
import fs from "fs";
import path from "path";
import { Collection, Document, ObjectId, WithId } from "mongodb";
const ffmpeg = require("fluent-ffmpeg");

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");

class VideoService {
  async getVideoById(id: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    return videoCollection.findOne({ _id: new ObjectId(id) });
  }

  async getFinishedVideosFromUser(userId: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const videos = await videoCollection.find({ requested_by: userId, status: "Finished" }).toArray();
    const updatePromises = videos.map((video) => {
      if (!video.size) {
        return this.updateVideoSize(video, videoCollection);
      }
    });
    await Promise.all(updatePromises);
    return videos;
  }

  updateVideoSize(video: WithId<Document>, videoCollection: Collection<Document>) {
    return new Promise((resolve, reject) => {
      const filePath = path.join(VIDEO_PATH, video.filename);
      if (fs.existsSync(filePath)) {
        const stat = fs.statSync(filePath);
        const fileSizeInBytes = stat.size;
        const fileSizeInMegabytes = fileSizeInBytes / (1024 * 1024);
        video.size = `${fileSizeInMegabytes.toFixed(2)} MB`;
        videoCollection
          .updateOne({ _id: video._id }, { $set: { size: video.size } })
          .then(() => resolve(undefined))
          .catch((err) => reject(err));
      } else {
        resolve(undefined);
      }
    });
  }

  getVideoSize(videoPath: string): Promise<number> {
    return new Promise((resolve, reject) => {
      fs.stat(videoPath, (err, stats) => {
        if (err) {
          reject(err);
        } else {
          const sizeInMB = stats.size / (1024 * 1024);
          resolve(sizeInMB);
        }
      });
    });
  }

  generateThumbnail(videoPath: string, thumbnailPath: string): Promise<void> {
    return new Promise((resolve, reject) => {
      ffmpeg(videoPath)
        .on("end", resolve)
        .on("error", reject)
        .screenshots({
          timestamps: ["15%"],
          filename: thumbnailPath,
          folder: "public/thumbnail",
          size: "320x240",
        });
    });
  }

  async getVideosFromUser(userId: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const videos = await videoCollection.find({ requested_by: userId }).toArray();
    const updatePromises = videos.map((video) => {
      if (!video.size) {
        return this.updateVideoSize(video, videoCollection);
      }
    });
    await Promise.all(updatePromises);
    return videos;
  }

  async getVideosByUser(userId: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const videos = videoCollection.find({ broadcaster_id: userId, status: "Finished" }).toArray();
    return videos;
  }
}

export default VideoService;
