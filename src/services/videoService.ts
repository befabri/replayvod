import { getDbInstance } from "../models/db";
import fs from "fs";
import path from "path";
import { ObjectId } from "mongodb";

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");

class VideoService {
  async getVideoById(id: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    return videoCollection.findOne({ _id: new ObjectId(id) });
  }

  async getFinishedVideosByUser(userId: string) {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const videos = await videoCollection.find({ requested_by: userId, status: "Finished" }).toArray();
    for (let video of videos) {
      if (!video.size) {
        const filePath = path.join(VIDEO_PATH, video.filename);
        if (fs.existsSync(filePath)) {
          const stat = fs.statSync(filePath);
          const fileSizeInBytes = stat.size;
          const fileSizeInMegabytes = fileSizeInBytes / (1024 * 1024);
          video.size = `${fileSizeInMegabytes.toFixed(2)} MB`;
          await videoCollection.updateOne({ _id: video._id }, { $set: { size: video.size } });
        }
      }
    }
    return videos;
  }
}

export default VideoService;
