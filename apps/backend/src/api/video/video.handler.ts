import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import fs from "fs";
import { Status } from "@prisma/client";

const RANGE_LIMIT = 500 * 1024;

interface Params extends RouteGenericInterface {
    Params: {
        id: string;
        login?: string;
        filename?: string;
        name?: string;
        broadcasterLogin?: string;
    };
}

interface ParamsVideo extends RouteGenericInterface {
    Params: {
        id: number;
    };
}

export class VideoHandler {
    playVideo = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const videoId = req.params.id;
        const repository = req.server.video.repository;
        if (videoId === "undefined") {
            return reply.status(400).send({ message: "Invalid video id" });
        }
        const videoIdRequest = parseInt(videoId, 10);
        if (isNaN(videoIdRequest)) {
            return reply.status(400).send({ message: "Invalid video id" });
        }
        const video = await repository.getVideoById(videoIdRequest);
        if (!video) {
            return reply.status(404).send({ message: "Video not found in database" });
        }
        const videoPath = repository.getVideoPath(video.displayName, video.filename);
        if (!fs.existsSync(videoPath)) {
            return reply.status(404).send({ message: "File not found on server" });
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

    getVideos = async (req: FastifyRequest, reply: FastifyReply) => {
        const repository = req.server.video.repository;
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const videos = await repository.getVideosFromUser(userId);
        reply.send(videos);
    };

    getVideo = async (req: FastifyRequest<ParamsVideo>, reply: FastifyReply) => {
        const videoId = req.params.id;
        const repository = req.server.video.repository;
        if (!videoId) {
            return reply.status(400).send({ message: "Invalid video id" });
        }
        const video = await repository.getVideoById(videoId);
        reply.send(video);
    };

    getFinishedVideos = async (req: FastifyRequest, reply: FastifyReply) => {
        const repository = req.server.video.repository;
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const videos = await repository.getVideosFromUser(userId, Status.DONE);
        reply.send(videos);
    };

    getPendingVideos = async (req: FastifyRequest, reply: FastifyReply) => {
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        const repository = req.server.video.repository;
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const videos = await repository.getVideosFromUser(userId, Status.PENDING);
        reply.send(videos);
    };

    getVideosByCategory = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const repository = req.server.video.repository;
        const userRepository = req.server.user.repository;
        const categoryRepository = req.server.category.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const categoryName = req.params.name;
        if (!categoryName) {
            return reply.status(400).send({ message: "Invalid category name" });
        }
        const category = await categoryRepository.getCategoryByName(categoryName);
        if (!category) {
            return reply.status(401).send(null);
        }
        const videos = await repository.getVideosByCategory(category.id, userId);
        reply.send({ videos: videos, category: category });
    };

    getVideoStatistics = async (req: FastifyRequest, reply: FastifyReply) => {
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        const repository = req.server.video.repository;
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const videos = await repository.getVideoStatistics(userId);
        reply.send(videos);
    };

    getChannelVideos = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const repository = req.server.video.repository;
        const channelRepository = req.server.channel.repository;
        const loginName = req.params.broadcasterLogin;
        if (!loginName || loginName === "undefined") {
            return reply.status(400).send({ message: "Invalid broadcaster login" });
        }
        const channel = await channelRepository.getChannelByName(loginName);
        if (!channel) {
            return reply.status(404).send({ message: "Channel not found" });
        }
        const videos = await repository.getVideosByChannel(channel.broadcasterId);
        reply.send({ videos: videos, channel: channel });
    };

    generateMissingThumbnail = async (req: FastifyRequest, reply: FastifyReply) => {
        try {
            const repository = req.server.video.repository;
            const thumb = await repository.generateMissingThumbnailsAndUpdate();
            reply.send(thumb);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };
}
