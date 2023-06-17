import { Request, Response } from "express";
import fs from "fs";
import path from "path";
import { v4 as uuidv4 } from "uuid";

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");

export const playVideo = async (req: Request, res: Response) => {
  const videoPath = VIDEO_PATH + "/video1.mp4";
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
  const videoFolderPath = VIDEO_PATH;
  const files = fs.readdirSync(videoFolderPath);

  const videos = files.map((file) => {
    const filePath = path.join(videoFolderPath, file);
    const stat = fs.statSync(filePath);
    const fileSizeInBytes = stat.size;
    const fileSizeInMegabytes = fileSizeInBytes / (1024 * 1024);
    return {
      id: uuidv4(),
      name: file,
      size: `${fileSizeInMegabytes.toFixed(2)} MB`,
    };
  });

  res.json(videos);
};
