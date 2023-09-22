import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { logger as rootLogger } from "../app";
import fs from "fs";
import path from "path";
import { channelService, videoService, userService } from "../services";
import { Status } from "@prisma/client";

const logger = rootLogger.child({ domain: "video", service: "videoController" });

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");
const PUBLIC_DIR = process.env.PUBLIC_DIR || VIDEO_PATH;
const RANGE_LIMIT = 500 * 1024;

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
    const videoStats = fs.statSync(videoPath);
    const videoRange = req.headers.range;
    if (videoRange) {
        const videoSize = fs.statSync(videoPath).size;
        const parts = videoRange.replace(/bytes=/, "").split("-");
        const start = parseInt(parts[0], 10);
        const end = parts[1]
            ? parseInt(parts[1], 10)
            : start + RANGE_LIMIT < videoSize - 1
            ? start + RANGE_LIMIT
            : videoSize - 1;
        const chunkSize = end - start + 1;
        const file = fs.createReadStream(videoPath, { start, end });

        await reply
            .code(206)
            .header("Content-Type", "application/octet-stream")
            .header("Content-Range", `bytes ${start}-${end}/${videoSize}`)
            .header("Accept-Ranges", "bytes")
            .header("Content-Length", chunkSize)
            .send(file);
    } else {
        reply.headers({
            "Content-Length": videoStats.size,
            "Content-Type": "video/mp4",
        });
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

// export const playVideo = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
//     const videoId = req.params.id;

//     if (videoId === "undefined") {
//         reply.status(400).send("Invalid video id");
//         return;
//     }
//     const videoIdRequest = parseInt(videoId, 10);
//     if (isNaN(videoIdRequest)) {
//         reply.status(400).send("Invalid video id");
//         return;
//     }
//     const video = await videoService.getVideoById(videoIdRequest);
//     if (!video) {
//         reply.status(404).send("Video not found in database");
//         return;
//     }
//     const videoPath = path.resolve(PUBLIC_DIR, "videos", video.displayName.toLowerCase(), video.filename);
//     if (!fs.existsSync(videoPath)) {
//         reply.status(404).send("File not found on server");
//         return;
//     }
//     logger.info("Reading video");
//     const videoStats = fs.statSync(videoPath);

//     const videoRange = req.headers.range;
//     console.log(videoRange);
//     if (videoRange) {
//         let [startString, endString] = videoRange.replace(/bytes=/, "").split("-");
//         let start = parseInt(startString, 10);
//         let end = endString ? parseInt(endString, 10) : videoStats.size - 1;
//         console.log(start, end);
//         if (!isNaN(start) && isNaN(end)) {
//             end = videoStats.size - 1;
//         }
//         if (isNaN(start) && !isNaN(end)) {
//             start = videoStats.size - end;
//             end = videoStats.size - 1;
//         }

//         if (start >= videoStats.size || end >= videoStats.size) {
//             reply
//                 .status(416)
//                 .headers({
//                     "Content-Range": `bytes */${videoStats.size}`,
//                 })
//                 .send();
//             return;
//         }
//         console.log("Sending headers with chunk:", end - start + 1);
//         reply.status(206).headers({
//             "Content-Range": `bytes ${start}-${end}/${videoStats.size}`,
//             "Accept-Ranges": "bytes",
//             "Content-Length": end - start + 1,
//             "Content-Type": "video/mp4",
//         });

//         const videoStream = fs.createReadStream(videoPath, { start, end });
//         videoStream.pipe(reply.raw);
//     } else {
//         reply.headers({
//             "Content-Length": videoStats.size,
//             "Content-Type": "video/mp4",
//         });
//         fs.createReadStream(videoPath).pipe(reply.raw);
//     }
// };
