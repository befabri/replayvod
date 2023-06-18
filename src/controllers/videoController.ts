import { Request, Response } from "express";
import fs from "fs";
import path from "path";
import VideoService from "../services/videoService";

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");

const videoService = new VideoService();

export const playVideo = async (req: Request, res: Response) => {
  const videoId = req.params.id;
  if (videoId === "undefined") {
    res.status(400).send("Invalid video id");
    return;
  }
  const video = await videoService.getVideoById(videoId);
  if (!video) {
    res.status(404).send("Video not found in database");
    return;
  }
  const videoPath = path.join(VIDEO_PATH, video.filename);
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
  const videos = await videoService.getFinishedVideosByUser(userId);
  res.json(videos);
};
