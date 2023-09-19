import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { logger as rootLogger } from "../app";
import fs from "fs";
import path from "path";
import { channelService, videoService, userService } from "../services";
import { Status } from "@prisma/client";

const logger = rootLogger.child({ service: "videoController" });

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");
const PUBLIC_DIR = process.env.PUBLIC_DIR || VIDEO_PATH;

interface Params extends RouteGenericInterface {
    Params: {
        id: string;
        login?: string;
        filename?: string;
    };
}

export const playVideo = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const videoId = req.params.id;

    if (videoId === "undefined") {
        reply.status(400).send("Invalid video id");
        return;
    }
    const videoIdRequest = parseInt(videoId, 10);
    if (isNaN(videoIdRequest)) {
        reply.status(400).send("Invalid video id");
        return;
    }
    const video = await videoService.getVideoById(videoIdRequest);
    if (!video) {
        reply.status(404).send("Video not found in database");
        return;
    }
    const videoPath = path.resolve(PUBLIC_DIR, "videos", video.displayName.toLowerCase(), video.filename);
    if (!fs.existsSync(videoPath)) {
        reply.status(404).send("File not found on server");
        return;
    }
    logger.info("Reading video");
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

        reply.code(206).headers(head);
        file.pipe(reply.raw);
    } else {
        const head = {
            "Content-Length": fileSize,
            "Content-Type": "video/mp4",
        };

        reply.code(200).headers(head);
        fs.createReadStream(videoPath).pipe(reply.raw);
    }
};

export const getVideos = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    if (!userId) {
        reply.status(401).send("Unauthorized");
        return;
    }
    const videos = await videoService.getVideosFromUser(userId);
    reply.send(videos);
};

export const getFinishedVideos = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    if (!userId) {
        reply.status(401).send("Unauthorized");
        return;
    }
    const videos = await videoService.getVideosFromUser(userId, Status.DONE);
    reply.send(videos);
};

export const getChannelVideos = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const broadcasterId = req.params.id;
    if (broadcasterId === "undefined") {
        reply.status(400).send("Invalid video id");
        return;
    }
    const channel = await channelService.getChannelDetailDB(broadcasterId);
    if (!channel) {
        reply.status(404).send("User not found");
        return;
    }
    const videos = await videoService.getVideosByChannel(broadcasterId);
    reply.send(videos);
};

export const generateMissingThumbnail = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const thumb = await videoService.generateMissingThumbnailsAndUpdate();
        reply.send(thumb);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};

// export const getThumbnail = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
//     const { login, filename } = req.params;
//     if (!login || !filename) {
//         return reply.status(400).send("Invalid parameters: Both login and filename are required");
//     }

//     const imagePath = path.resolve(PUBLIC_DIR, "thumbnail", login, filename);
//     try {
//         const stat = await fsPromises.stat(imagePath);

//         const oneDayInSeconds = 86400;
//         reply.header("Cache-Control", `public, max-age=${oneDayInSeconds}`);
//         reply.header("Last-Modified", stat.mtime.toUTCString());

//         const ifModifiedSince = req.headers["if-modified-since"];
//         if (ifModifiedSince && new Date(ifModifiedSince).getTime() >= stat.mtime.getTime()) {
//             return reply.status(304).send();
//         }

//         const mimeType = mime.getType(imagePath) || "application/octet-stream";
//         reply.header("Content-Type", mimeType);
//         reply.header("Content-Length", String(stat.size));
//         const stream = createReadStreamWithHandlers(imagePath);
//         return reply.send(stream);
//     } catch (err) {
//         console.error(`Error accessing the file: ${err.message}`);
//         if (err.code === "ENOENT") {
//             return reply.status(404).send("File not found");
//         } else {
//             return reply.status(500).send("Error accessing the file");
//         }
//     }
// };

// const createReadStreamWithHandlers = (imagePath: string) => {
//     const stream = fs.createReadStream(imagePath);
//     stream.on("error", (error) => {
//         console.error(`Error reading the stream: ${error.message}`);
//     });

//     stream.on("close", () => {
//         console.log("ReadStream closed");
//     });

//     return stream;
// };
