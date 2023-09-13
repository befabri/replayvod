import { FastifyInstance, FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { logger as rootLogger } from "../app";
import fs from "fs";
import path from "path";
import { channelService, videoService } from "../services";
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
    const userId = req.session.user.data[0].id;
    const videos = await videoService.getVideosFromUser(userId);
    reply.send(videos);
};

export const getFinishedVideos = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = req.session.user.data[0].id;
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

export const getThumbnail = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    // TODO Verify permission / verify into mongo
    const { login, filename } = req.params;
    if (!login || !filename) {
        return reply.status(400).send("Invalid parameters: Both login and filename are required");
    }
    const imagePath = path.resolve(PUBLIC_DIR, "thumbnail", login, filename);
    fs.stat(imagePath, (err, stat) => {
        if (err) {
            if (err.code === "ENOENT") {
                return reply.status(404).send("File not found");
            } else {
                return reply.status(500).send("Error accessing the file");
            }
        }
        const stream = fs.createReadStream(imagePath);
        stream.on("open", () => {
            reply.header("Content-Type", "image/jpeg");
            reply.header("Content-Length", String(stat.size));
            stream.pipe(reply.raw);
        });
        stream.on("error", (streamErr) => {
            return reply.status(500).send(`Error streaming the image: ${streamErr.message}`);
        });
    });
};
