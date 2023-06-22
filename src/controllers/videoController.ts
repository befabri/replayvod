import { Request, Response } from "express";
import fs from "fs";
import path from "path";
import VideoService from "../services/videoService";
import UserService from "../services/userService";
import { Video } from "../models/videoModel";

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");

const videoService = new VideoService();
const userService = new UserService();

export const playVideo = async (req: Request, res: Response) => {
  const videoId = req.params.id;
  if (videoId === "undefined") {
    res.status(400).send("Invalid video id");
    return;
  }
  const video: Video = await videoService.getVideoById(videoId);
  if (!video) {
    res.status(404).send("Video not found in database");
    return;
  }
  const videoPath = `public/videos/${video.display_name.toLowerCase()}/${video.filename}`;
  if (!fs.existsSync(videoPath)) {
    res.status(404).send("File not found on server");
    return;
  }
  const stat = fs.statSync(videoPath);
  const fileSize = stat.size;
  const range = req.headers.range;

  if (range) {
    const parts = range.replace(/bytes=/, "").split("-");
    const start = parseInt(parts[0], 10);
    const end = parts[1] ? parseInt(parts[1], 10) : fileSize - 1;

    const chunksize = end - start + 1;
    const file = fs.createReadStream(videoPath, { start, end });
    const head = {
      "Content-Range": `bytes ${start}-${end}/${fileSize}`,
      "Accept-Ranges": "bytes",
      "Content-Length": chunksize,
      "Content-Type": "video/mp4",
    };

    res.writeHead(206, head);
    file.pipe(res);
  } else {
    const head = {
      "Content-Length": fileSize,
      "Content-Type": "video/mp4",
    };

    res.writeHead(200, head);
    fs.createReadStream(videoPath).pipe(res);
  }
};

export const getVideos = async (req: Request, res: Response) => {
  const userId = req.session.passport.user.data[0].id;
  const videos = await videoService.getVideosFromUser(userId);
  res.json(videos);
};

export const getFinishedVideos = async (req: Request, res: Response) => {
  const userId = req.session.passport.user.data[0].id;
  const videos = await videoService.getFinishedVideosFromUser(userId);
  res.json(videos);
};

export const getUserVideos = async (req: Request, res: Response) => {
  const userId = req.params.id;
  if (userId === "undefined") {
    res.status(400).send("Invalid video id");
    return;
  }
  const user = await userService.getUserDetailDB(userId);
  if (!user) {
    res.status(404).send("User not found");
    return;
  }
  const videos = await videoService.getVideosByUser(userId);
  res.json(videos);
};

export const generateMissingThumbnail = async (req: Request, res: Response) => {
  try {
    const result = await videoService.generateMissingThumbnailsAndUpdate();
    res.json(result);
  } catch (error) {
    res.status(500).send("Internal server error");
  }
};
