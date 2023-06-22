import { getDbInstance } from "../models/db";
import fs from "fs";
import path from "path";
import { Collection, Document, ObjectId, WithId } from "mongodb";
import ffmpeg from "fluent-ffmpeg";
import { Video } from "../models/videoModel";
import { exec } from "child_process";

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");

class VideoService {
  async getVideoById(id: string): Promise<Video | null> {
    const db = await getDbInstance();
    const videoCollection: Collection<Video> = db.collection("videos");
    return videoCollection.findOne({ id: id });
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
          const sizeInMB = parseFloat((stats.size / (1024 * 1024)).toFixed(2));
          resolve(sizeInMB);
        }
      });
    });
  }

  generateThumbnail(videoPath: string, thumbnailPath: string): Promise<void> {
    return new Promise((resolve, reject) => {
      const command = `ffmpeg -i "${videoPath}" -ss 00:00:15 -vframes 1 -s 320x240 "${thumbnailPath}"`;
      exec(command, (error, stdout, stderr) => {
        if (error) {
          reject(error);
        } else {
          resolve();
        }
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
