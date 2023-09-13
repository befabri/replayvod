import fs from "fs";
import path from "path";
import ffmpeg from "fluent-ffmpeg";
import { Status, Video } from "@prisma/client";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "videoService" });

export const getVideoById = async (id: number): Promise<Video | null> => {
    return prisma.video.findUnique({
        where: { id: id },
    });
};

export const getVideosFromUser = async (userId: string, status?: Status) => {
    const videoRequests = await prisma.videoRequest.findMany({
        where: { userId },
        select: { videoId: true },
    });

    const videoIds = videoRequests.map((request) => request.videoId);

    const videos = await prisma.video.findMany({
        where: {
            id: { in: videoIds },
            ...(status && { status }),
        },
    });

    const videosWithoutSize = videos.filter((video) => !video.size);
    await Promise.all(videosWithoutSize.map(updateVideoSize));

    return videos;
};

export const updateVideoSize = async (video: Video) => {
    const filePath = path.resolve(
        process.env.PUBLIC_DIR,
        "videos",
        video.displayName.toLowerCase(),
        video.filename
    );
    if (fs.existsSync(filePath)) {
        const stat = fs.statSync(filePath);
        const fileSizeInBytes = stat.size;
        const fileSizeInMegabytes = fileSizeInBytes / (1024 * 1024);
        video.size = fileSizeInMegabytes;
        await prisma.video.update({
            where: { id: video.id },
            data: { size: video.size },
        });
    }
};

export const getVideoSize = (videoPath: string): Promise<number> => {
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
};

export const generateThumbnail = (videoPath: string, thumbnailPath: string, timestamps: string): Promise<void> => {
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
};

export const generateSingleThumbnail = async (videoPath: string, videoName: string, login: string) => {
    const duration = await getVideoDuration(videoPath);
    const thumbnailName = videoName.replace(".mp4", ".jpg");
    const directoryPath = path.resolve(process.env.PUBLIC_DIR, "thumbnail", login);
    if (!fs.existsSync(directoryPath)) {
        fs.mkdirSync(directoryPath, { recursive: true });
    }
    const thumbnailPath = videoPath.replace("videos", "thumbnail").replace(videoName, thumbnailName);

    let timestamp = 300;
    for (let tries = 0; tries < 5; tries++) {
        try {
            await generateThumbnail(videoPath, thumbnailPath, secondsToTimestamp(timestamp));
            return getRelativePath(thumbnailPath);
        } catch (error) {
            if (error.message === "Image is a single color") {
                timestamp += 60;
                if (timestamp >= duration) {
                    timestamp -= duration - 3;
                }
            } else {
                logger.error(`Error generating thumbnail: ${error}`);
                return null;
            }
        }
    }

    return null;
};

export const generateMissingThumbnailsAndUpdate = async () => {
    // TODO verify the true duration before
    try {
        const videos = await prisma.video.findMany({
            where: { thumbnail: null, status: Status.DONE },
        });
        const promises = videos.map(async (video) => {
            const thumbnailPath = path.resolve(
                process.env.PUBLIC_DIR,
                "thumbnail",
                video.displayName.toLowerCase(),
                video.filename.replace(".mp4", ".jpg")
            );
            const videoPath = path.resolve(
                process.env.PUBLIC_DIR,
                "videos",
                video.displayName.toLowerCase(),
                video.filename
            );
            const duration = await getVideoDuration(videoPath);
            if (!fs.existsSync(path.dirname(thumbnailPath))) {
                fs.mkdirSync(path.dirname(thumbnailPath), { recursive: true });
            }
            let timestamp = 300;
            if (timestamp >= duration) {
                timestamp = 30;
            }
            for (let tries = 0; tries < 5; tries++) {
                try {
                    await generateThumbnail(videoPath, thumbnailPath, secondsToTimestamp(timestamp));
                    const updatedVideo = await prisma.video.update({
                        where: {
                            id: video.id,
                        },
                        data: {
                            thumbnail: getRelativePath(thumbnailPath),
                        },
                    });

                    break;
                } catch (error) {
                    if (error.message === "Image is a single color") {
                        timestamp += 60;
                        if (timestamp >= duration) {
                            timestamp -= duration - 3;
                        }
                    } else {
                        logger.error("Error generating thumbnail or updating collection:", error);
                    }
                }
            }
        });
        await Promise.all(promises);
        return prisma.video.findMany({
            where: {
                thumbnail: {
                    not: {
                        equals: null,
                    },
                },
            },
        });
    } catch (error) {
        logger.error("Error generating missing thumbnails and updating collection:", error);
        return [];
    }
};

export const isVideoCorrupt = (metadata) => {
    const videoStream = metadata.streams.find((s) => s.codec_type === "video");
    const audioStream = metadata.streams.find((s) => s.codec_type === "audio");
    const duration = metadata.format.duration;
    if (!videoStream || !audioStream) {
        logger.error("Missing video or audio stream");
        throw new Error("Missing video or audio stream");
    }
    const videoDuration = parseFloat(duration);
    const streamDuration = parseFloat(videoStream.duration);
    const discrepancy = Math.abs(videoDuration - streamDuration);
    logger.info(`Discrepancy: ${discrepancy}, is it greater than 50? ${discrepancy > 50}`);
    return discrepancy > 50;
};

export const fixMalformedVideos = async () => {
    const videos = await prisma.video.findMany();
    for (const video of videos) {
        const videoPath = path.resolve(
            process.env.PUBLIC_DIR,
            "videos",
            video.displayName.toLowerCase(),
            video.filename
        );
        if (fs.existsSync(videoPath)) {
            try {
                logger.info(`Processing video: ${videoPath}`);
                let metadata = await getMetadata(videoPath);
                if (isVideoCorrupt(metadata)) {
                    logger.info(`Video might be corrupt. Attempting to fix...`);
                    const fixedVideoPath = videoPath.replace(".mp4", "FIX.mp4");
                    await fixVideo(videoPath, fixedVideoPath);
                    metadata = await getMetadata(fixedVideoPath);
                    if (isVideoCorrupt(metadata)) {
                        logger.error(`Video is still corrupt after fixing.`);
                    } else {
                        logger.info(`Video has been successfully fixed.`);
                        const tempOriginalPath = videoPath.replace(".mp4", "TEMP.mp4");
                        fs.renameSync(videoPath, tempOriginalPath);
                        fs.renameSync(fixedVideoPath, videoPath);
                        fs.unlinkSync(tempOriginalPath);
                        logger.info(`Successfully replaced the corrupt video with the fixed one.`);
                    }
                } else {
                    logger.info(`Video seems fine, no actions taken.`);
                }
            } catch (error) {
                logger.error(`Error processing video at path ${videoPath}: ${error.message}`);
            }
        } else {
            logger.warn(`Video does not exist at path: ${videoPath}`);
        }
    }
};

export const getMaxFrames = (videoPath: string): Promise<number> => {
    return new Promise((resolve, reject) => {
        ffmpeg.ffprobe(videoPath, (err, metadata) => {
            if (err) {
                reject(err);
            } else {
                const frameCount = metadata.streams[0]?.nb_frames;
                resolve(frameCount);
            }
        });
    });
};

export const getMetadata = (videoPath) => {
    return new Promise((resolve, reject) => {
        ffmpeg.ffprobe(videoPath, (err, metadata) => {
            if (err) {
                reject(err);
            } else {
                resolve(metadata);
            }
        });
    });
};

export const fixVideo = (inputVideoPath: string, outputVideoPath: string): Promise<void> => {
    return new Promise((resolve, reject) => {
        ffmpeg(inputVideoPath)
            .outputOptions("-vcodec copy")
            .outputOptions("-acodec copy")
            .save(outputVideoPath)
            .on("end", resolve)
            .on("error", reject);
    });
};

export const getRelativePath = (fullPath: string): string => {
    let pathParts = fullPath.split(path.sep);
    let relativePath = pathParts.slice(-2).join("/");
    return relativePath;
};

export const secondsToTimestamp = (seconds: number) => {
    const hrs = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds - hrs * 3600) / 60);
    const secs = seconds - hrs * 3600 - mins * 60;
    return `${hrs}:${mins}:${secs}`;
};

export const getVideoDuration = (videoPath: string): Promise<number> => {
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
};

export const updateVideoData = async (
    filename: string,
    endAt: Date,
    thumbnail: string,
    size: number,
    duration: number
) => {
    return prisma.video.updateMany({
        where: {
            filename: filename,
        },
        data: {
            downloadedAt: endAt,
            status: Status.DONE,
            thumbnail: thumbnail,
            size: size,
            duration: duration,
        },
    });
};

export const getVideosByChannel = async (broadcaster_id: string) => {
    return prisma.video.findMany({
        where: {
            broadcasterId: broadcaster_id,
            status: Status.DONE,
        },
    });
};

export default {
    getVideoById,
    updateVideoSize,
    getVideoSize,
    generateThumbnail,
    generateSingleThumbnail,
    generateMissingThumbnailsAndUpdate,
    isVideoCorrupt,
    fixMalformedVideos,
    getMaxFrames,
    getMetadata,
    fixVideo,
    getRelativePath,
    secondsToTimestamp,
    getVideoDuration,
    getVideosFromUser,
    updateVideoData,
    getVideosByChannel,
};
