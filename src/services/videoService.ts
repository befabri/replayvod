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

  generateThumbnail(videoPath: string, thumbnailPath: string, timestamps: string): Promise<void> {
    return new Promise((resolve, reject) => {
      ffmpeg(videoPath)
        .on("end", resolve)
        .on("error", reject)
        .screenshots({
          timestamps: [timestamps],
          filename: path.basename(thumbnailPath),
          folder: path.dirname(thumbnailPath),
          size: "1920x1080",
        });
    });
  }

  async generateSingleThumbnail(videoPath: string, videoName: string, login: string) {
    const duration = await this.getVideoDuration(videoPath);
    const thumbnailName = videoName.replace(".mp4", ".jpg");
    const directoryPath = path.join("public", "thumbnail", login);
    if (!fs.existsSync(directoryPath)) {
      fs.mkdirSync(directoryPath, { recursive: true });
    }
    const thumbnailPath = videoPath.replace("videos", "thumbnail").replace(videoName, thumbnailName);

    let timestamp = 300;
    for (let tries = 0; tries < 5; tries++) {
      try {
        await this.generateThumbnail(videoPath, thumbnailPath, this.secondsToTimestamp(timestamp));
        return thumbnailPath;
      } catch (error) {
        if (error.message === "Image is a single color") {
          timestamp += 60;
          if (timestamp >= duration) {
            timestamp -= duration - 3;
          }
        } else {
          console.error("Error generating thumbnail:", error);
          return null;
        }
      }
    }

    return null;
  }

  async generateMissingThumbnailsAndUpdate() {
    try {
      const db = await getDbInstance();
      const videoCollection = db.collection("videos");
      const videos = await videoCollection.find({ thumbnail: null, status: "Finished" }).toArray();
      const promises = videos.map(async (video) => {
        const thumbnailPath = path.join(
          "public",
          "thumbnail",
          video.display_name.toLowerCase(),
          video.filename.replace(".mp4", ".jpg")
        );
        const videoPath = path.join("public", "videos", video.display_name.toLowerCase(), video.filename);
        const duration = await this.getVideoDuration(videoPath);
        if (!fs.existsSync(path.dirname(thumbnailPath))) {
          fs.mkdirSync(path.dirname(thumbnailPath), { recursive: true });
        }
        let timestamp = 300;
        if (timestamp >= duration) {
          timestamp = 30;
        }
        console.log(timestamp);
        for (let tries = 0; tries < 5; tries++) {
          try {
            await this.generateThumbnail(videoPath, thumbnailPath, this.secondsToTimestamp(timestamp));
            await videoCollection.updateOne(
              { _id: video._id },
              { $set: { thumbnail: this.getRelativePath(thumbnailPath) } }
            );
            break;
          } catch (error) {
            if (error.message === "Image is a single color") {
              timestamp += 60;
              if (timestamp >= duration) {
                timestamp -= duration - 3;
              }
            } else {
              console.error("Error generating thumbnail or updating collection:", error);
            }
          }
        }
      });
      await Promise.all(promises);
      return videoCollection.find({ thumbnail: { $ne: null } }).toArray();
    } catch (error) {
      console.error("Error generating missing thumbnails and updating collection:", error);
      return [];
    }
  }

  getRelativePath(fullPath: string): string {
    let pathParts = fullPath.split(path.sep);
    let relativePath = pathParts.slice(2).join("/");
    return relativePath;
  }

  secondsToTimestamp(seconds: number) {
    const hrs = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds - hrs * 3600) / 60);
    const secs = seconds - hrs * 3600 - mins * 60;
    return `${hrs}:${mins}:${secs}`;
  }

  getVideoDuration(videoPath: string): Promise<number> {
    return new Promise((resolve, reject) => {
      ffmpeg.ffprobe(videoPath, (err, metadata) => {
        if (err) {
          reject(err);
        } else {
          const durationInSeconds = metadata.format.duration;
          resolve(durationInSeconds);
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

  async updateVideoData(filename: string, endAt: Date, thumbnail: string, size: number, duration: number) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    return videoCollection.updateOne(
      { filename: filename },
      {
        $set: {
          downloaded_at: endAt,
          status: "Finished",
          thumbnail: thumbnail,
          size: size,
          duration: duration,
        },
      }
    );
  }

  async getVideosByUser(userId: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const videos = videoCollection.find({ broadcaster_id: userId, status: "Finished" }).toArray();
    return videos;
  }
}

export default VideoService;
