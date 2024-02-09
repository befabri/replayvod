import fs from "fs";
import path from "path";
import ffmpeg, { FfprobeFormat } from "fluent-ffmpeg";
import { Channel, Quality, Status, Video } from "@prisma/client";
import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { Resolution, VideoQuality } from "../../models/dowloadModel";
import { tagService, titleService } from "../../services";
import { categoryFeature } from "../category";
import { transformVideo, videoDTO } from "./video.DTO";
import { DateTime } from "luxon";
import { StreamDTO } from "../channel/channel.DTO";
const logger = rootLogger.child({ domain: "video", service: "videoService" });

const VIDEO_PATH = path.resolve(__dirname, "..", "..", "public", "videos");
const PUBLIC_DIR = process.env.PUBLIC_DIR || VIDEO_PATH;

export const getVideoById = async (id: number): Promise<videoDTO | null> => {
    const video = await prisma.video.findUnique({
        where: { id: id },
        select: videoSelectConfig,
    });
    if (!video) {
        return null;
    }
    if (!video.size) {
        await updateVideoSize(video);
    }
    const transformed = transformVideo([video]);
    return transformed[0];
};

export const getVideosByCategory = async (categoryId: string, userId: string): Promise<videoDTO[]> => {
    const videos = await prisma.video.findMany({
        where: {
            AND: [
                {
                    videoCategory: {
                        some: {
                            categoryId: categoryId,
                        },
                    },
                },
                {
                    VideoRequest: {
                        some: {
                            userId: userId,
                        },
                    },
                },
                {
                    status: Status.DONE,
                },
            ],
        },
        select: videoSelectConfig,
    });

    const videosWithoutSize = videos.filter((video) => !video.size);
    await Promise.all(videosWithoutSize.map(updateVideoSize));
    const transformed = transformVideo(videos);
    return transformed;
};

export const getVideoStatistics = async (userId: string) => {
    const videoRequests = await prisma.videoRequest.findMany({
        where: { userId },
        select: { videoId: true },
    });

    const videoIds = videoRequests.map((request) => request.videoId);

    const totalDoneVideos = await prisma.video.count({
        where: {
            id: { in: videoIds },
            status: Status.DONE,
        },
    });

    const totalDownloadingVideos = await prisma.video.count({
        where: {
            id: { in: videoIds },
            status: Status.PENDING,
        },
    });

    const totalFailedVideos = await prisma.video.count({
        where: {
            id: { in: videoIds },
            status: Status.FAILED,
        },
    });

    return {
        totalDoneVideos,
        totalDownloadingVideos,
        totalFailedVideos,
    };
};

export const getVideosFromUser = async (userId: string, status?: Status): Promise<videoDTO[]> => {
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
        orderBy: { downloadedAt: "desc" },
        select: videoSelectConfig,
    });
    const videosWithoutSize = videos.filter((video) => !video.size);
    await Promise.all(videosWithoutSize.map(updateVideoSize));
    const transformed = transformVideo(videos);
    return transformed;
};

export const mapVideoQualityToQuality = (input: string): Quality => {
    switch (input) {
        case VideoQuality.LOW:
            return Quality.LOW;
        case VideoQuality.MEDIUM:
            return Quality.MEDIUM;
        case VideoQuality.HIGH:
            return Quality.HIGH;
        default:
            return Quality.HIGH;
    }
};

export const mapQualityToVideoQuality = (quality: Quality): Resolution => {
    switch (quality) {
        case Quality.LOW:
            return VideoQuality.LOW;
        case Quality.MEDIUM:
            return VideoQuality.MEDIUM;
        case Quality.HIGH:
            return VideoQuality.HIGH;
        default:
            return VideoQuality.HIGH;
    }
};

export const updateVideoSize = async (video: Video) => {
    const filePath = path.resolve(PUBLIC_DIR, "videos", video.displayName.toLowerCase(), video.filename);
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
    const duration = await getVideoDuration(videoPath); // TODO when is 0
    const thumbnailName = videoName.replace(".mp4", ".jpg");
    const directoryPath = path.resolve(PUBLIC_DIR, "thumbnail", login);
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
            if (error instanceof Error) {
                if (error.message === "Image is a single color") {
                    timestamp += 60;
                    if (timestamp >= duration) {
                        timestamp -= duration - 3;
                    }
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
    logger.info("Generate missing thumbnails...");
    // TODO verify the true duration before
    // TODO verify if the thumbnail in video exist, if not generate it
    try {
        const videos = await prisma.video.findMany({
            where: { thumbnail: null, status: Status.DONE },
        });
        const promises = videos.map(async (video) => {
            const thumbnailPath = path.resolve(
                PUBLIC_DIR,
                "thumbnail",
                video.displayName.toLowerCase(),
                video.filename.replace(".mp4", ".jpg")
            );
            const videoPath = path.resolve(PUBLIC_DIR, "videos", video.displayName.toLowerCase(), video.filename);
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
                    await prisma.video.update({
                        where: {
                            id: video.id,
                        },
                        data: {
                            thumbnail: getRelativePath(thumbnailPath),
                        },
                    });

                    break;
                } catch (error) {
                    if (error instanceof Error) {
                        if (error.message === "Image is a single color") {
                            timestamp += 60;
                            if (timestamp >= duration) {
                                timestamp -= duration - 3;
                            }
                        }
                    } else {
                        logger.error(`Error generating thumbnail or updating collection: ${error}`);
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
        logger.error(`Error generating missing thumbnails and updating collection: ${error}`);
        return [];
    }
};

export const isVideoCorrupt = (metadata: ffmpeg.FfprobeFormat) => {
    const videoStream = metadata.streams.find((s: { codec_type: string }) => s.codec_type === "video");
    const audioStream = metadata.streams.find((s: { codec_type: string }) => s.codec_type === "audio");
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
    const videos = await prisma.video.findMany({
        where: { status: Status.DONE },
    });
    for (const video of videos) {
        const videoPath = path.resolve(PUBLIC_DIR, "videos", video.displayName.toLowerCase(), video.filename);
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
                if (error instanceof Error) {
                    logger.error(`Error processing video at path ${videoPath}: ${error.message}`);
                } else {
                    logger.error(`Error processing video at path ${videoPath}`);
                }
            }
        } else {
            logger.warn(`Video does not exist at path: ${videoPath}`);
        }
    }
};

export const getMaxFrames = (videoPath: string) => {
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

export const getMetadata = (videoPath: string): Promise<FfprobeFormat> => {
    return new Promise((resolve, reject) => {
        ffmpeg.ffprobe(videoPath, (err, metadata) => {
            if (err) {
                reject(err);
            } else {
                resolve(metadata as FfprobeFormat);
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
                resolve(durationInSeconds || 0);
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

export const getVideosByChannel = async (broadcaster_id: string): Promise<videoDTO[]> => {
    const videos = await prisma.video.findMany({
        where: {
            broadcasterId: broadcaster_id,
            status: Status.DONE,
        },
        orderBy: { downloadedAt: "desc" },
        select: videoSelectConfig,
    });
    const videosWithoutSize = videos.filter((video) => !video.size);
    await Promise.all(videosWithoutSize.map(updateVideoSize));
    const transformed = transformVideo(videos);
    return transformed;
};

export const saveVideoInfo = async ({
    userRequesting,
    channel,
    videoName,
    startAt,
    status,
    jobId,
    stream,
    videoQuality,
}: {
    userRequesting: string;
    channel: Channel;
    videoName: string;
    startAt: Date;
    status: Status;
    jobId: string;
    stream: StreamDTO;
    videoQuality: Quality;
}) => {
    try {
        const video = await prisma.video.create({
            data: {
                filename: videoName,
                status: status,
                displayName: channel.displayName,
                startDownloadAt: startAt,
                viewerCount: stream.viewerCount,
                language: stream.language,
                quality: videoQuality,
                channel: {
                    connect: {
                        broadcasterId: channel.broadcasterId,
                    },
                },
                job: {
                    connect: {
                        id: jobId,
                    },
                },
                stream: {
                    connect: {
                        id: stream.id,
                    },
                },
            },
        });
        await prisma.videoRequest.create({
            data: {
                video: {
                    connect: {
                        id: video.id,
                    },
                },
                user: {
                    connect: {
                        userId: userRequesting,
                    },
                },
            },
        });
        for (let title of stream.titles) {
            await titleService.createVideoTitle(video.id, title.titleId);
        }
        for (let category of stream.categories) {
            await categoryFeature.createVideoCategory(video.id, category.id);
        }
        for (let tag of stream.tags) {
            await tagService.addVideoTag(video.id, tag.name);
        }
    } catch (error) {
        throw new Error(`Error saving video: ${error}`);
    }
};

// const updateVideoTitle = async (videoId, titles) => {
//     const promises = titles.map((titleData) =>
//         prisma.videoTitle.upsert({
//             where: {
//                 videoId_titleId: {
//                     videoId: videoId,
//                     titleId: titleData.id,
//                 },
//             },
//             update: {},
//             create: {
//                 video: {
//                     connect: {
//                         id: videoId,
//                     },
//                 },
//                 title: {
//                     connect: {
//                         id: titleData.id,
//                     },
//                 },
//             },
//         })
//     );
//     try {
//         return await Promise.all(promises);
//     } catch (error) {
//         logger.error("Error updating video titles:", error);
//         throw error;
//     }
// };

// const updateVideoCategory = async (videoId: number, categories: any) => {
//     const promises = categories.map((categoryData: { id: string }) =>
//         prisma.videoCategory.upsert({
//             where: {
//                 videoId_categoryId: {
//                     videoId: videoId,
//                     categoryId: categoryData.id,
//                 },
//             },
//             update: {},
//             create: {
//                 video: {
//                     connect: {
//                         id: videoId,
//                     },
//                 },
//                 category: {
//                     connect: {
//                         id: categoryData.id,
//                     },
//                 },
//             },
//         })
//     );
//     try {
//         return await Promise.all(promises);
//     } catch (error) {
//         logger.error("Error updating video categories:", error);
//         throw error;
//     }
// };

// const updateVideoTag = async (videoId: number, tags: any) => {
//     const promises = tags.map((tagData: { name: string }) =>
//         prisma.videoTag.upsert({
//             where: {
//                 videoId_tagId: {
//                     videoId: videoId,
//                     tagId: tagData.name,
//                 },
//             },
//             update: {},
//             create: {
//                 video: {
//                     connect: {
//                         id: videoId,
//                     },
//                 },
//                 tag: {
//                     connect: {
//                         name: tagData.name,
//                     },
//                 },
//             },
//         })
//     );
//     try {
//         return await Promise.all(promises);
//     } catch (error) {
//         logger.error("Error updating video tags:", error);
//         throw error;
//     }
// };

// TODO: pourquoi mettre une date de fin ? -> Si fin autant mettre DONE directement
export const updateVideoInfo = async (videoName: string, endAt: Date, status: Status) => {
    return prisma.video.update({
        where: {
            filename: videoName,
        },
        data: {
            downloadedAt: endAt,
            status: status,
        },
    });
};

export const getVideoFilePath = (login: string) => {
    const currentDate = DateTime.now().toFormat("ddMMyyyy-HHmmss");
    const filename = `${login}_${currentDate}.mp4`;
    const directoryPath = path.resolve(PUBLIC_DIR, "videos", login);
    if (!fs.existsSync(directoryPath)) {
        fs.mkdirSync(directoryPath, { recursive: true });
    }
    return path.join(directoryPath, filename);
};

export const setVideoFailed = async () => {
    try {
        await prisma.video.updateMany({
            where: {
                status: "PENDING",
            },
            data: {
                status: "FAILED",
            },
        });
    } catch (error) {
        logger.error("Failed to update videos: %s", error);
    }
};

const videoSelectConfig = {
    id: true,
    filename: true,
    status: true,
    displayName: true,
    startDownloadAt: true,
    downloadedAt: true,
    viewerCount: true,
    language: true,
    quality: true,
    duration: true,
    size: true,
    thumbnail: true,
    broadcasterId: true,
    jobId: true,
    streamId: true,
    tags: {
        select: {
            tag: {
                select: {
                    name: true,
                },
            },
        },
    },
    titles: {
        select: {
            title: {
                select: {
                    name: true,
                },
            },
        },
    },
    videoCategory: {
        select: {
            category: {},
        },
    },
    channel: {
        select: {
            broadcasterId: true,
            broadcasterLogin: true,
            broadcasterName: true,
            displayName: true,
            broadcasterType: true,
            createdAt: true,
            description: true,
            offlineImageUrl: true,
            profileImageUrl: true,
            profilePicture: true,
            type: true,
            viewCount: true,
        },
    },
};
