import fs from "fs";
import path from "path";
import ffmpeg, { FfprobeFormat } from "fluent-ffmpeg";
import { Channel, PrismaClient, Quality, Status, Video } from "@prisma/client";
import { logger as rootLogger } from "../../app";
import { transformVideo, videoDTO } from "./video.dto";
import { DateTime } from "luxon";
import { StreamDTO } from "../channel/channel.dto";
import { THUMBNAIL_PATH, VIDEO_PATH } from "../../constants/constant.folder";
import { CategoryRepository } from "../category/category.repository";
import { Resolution, VideoQuality } from "../../models/model.download";
import { TagService } from "../../services/service.tag";
import { TitleService } from "../../services/service.title";
const logger = rootLogger.child({ domain: "video", service: "repository" });

export class VideoRepository {
    constructor(
        private db: PrismaClient,
        private categoryRepository: CategoryRepository,
        private tagService: TagService,
        private titleService: TitleService
    ) {}

    getVideoById = async (id: number): Promise<videoDTO | null> => {
        const video = await this.db.video.findUnique({
            where: { id: id },
            select: this.videoSelectConfig,
        });
        if (!video) {
            return null;
        }
        if (!video.size) {
            await this.updateVideoSize(video);
        }
        const transformed = transformVideo([video]);
        return transformed[0];
    };

    getVideoPath = (broadcasterLogin: string, filename: string) => {
        return path.resolve(VIDEO_PATH, broadcasterLogin.toLocaleLowerCase(), filename);
    };

    private getThumbnailPath = (broadcasterLogin: string, filename: string) => {
        return path.resolve(
            THUMBNAIL_PATH,
            broadcasterLogin.toLocaleLowerCase(),
            filename.replace(".mp4", ".jpg")
        );
    };

    getVideosByCategory = async (categoryId: string, userId: string): Promise<videoDTO[]> => {
        const videos = await this.db.video.findMany({
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
            select: this.videoSelectConfig,
        });

        const videosWithoutSize = videos.filter((video) => !video.size);
        await Promise.all(videosWithoutSize.map(this.updateVideoSize));
        const transformed = transformVideo(videos);
        return transformed;
    };

    getVideoStatistics = async (userId: string) => {
        const videoRequests = await this.db.videoRequest.findMany({
            where: { userId },
            select: { videoId: true },
        });

        const videoIds = videoRequests.map((request) => request.videoId);

        const totalDoneVideos = await this.db.video.count({
            where: {
                id: { in: videoIds },
                status: Status.DONE,
            },
        });

        const totalDownloadingVideos = await this.db.video.count({
            where: {
                id: { in: videoIds },
                status: Status.PENDING,
            },
        });

        const totalFailedVideos = await this.db.video.count({
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

    getVideosFromUser = async (userId: string, status?: Status): Promise<videoDTO[]> => {
        const videoRequests = await this.db.videoRequest.findMany({
            where: { userId },
            select: { videoId: true },
        });
        const videoIds = videoRequests.map((request) => request.videoId);
        const videos = await this.db.video.findMany({
            where: {
                id: { in: videoIds },
                ...(status && { status }),
            },
            orderBy: { downloadedAt: "desc" },
            select: this.videoSelectConfig,
        });
        const videosWithoutSize = videos.filter((video) => !video.size);
        await Promise.all(videosWithoutSize.map(this.updateVideoSize));
        const transformed = transformVideo(videos);
        return transformed;
    };

    mapVideoQualityToQuality = (input: string): Quality => {
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

    mapQualityToVideoQuality = (quality: Quality): Resolution => {
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

    updateVideoSize = async (video: Video) => {
        const filePath = this.getVideoPath(video.displayName, video.filename);
        if (fs.existsSync(filePath)) {
            const stat = fs.statSync(filePath);
            const fileSizeInBytes = stat.size;
            const fileSizeInMegabytes = fileSizeInBytes / (1024 * 1024);
            video.size = fileSizeInMegabytes;
            await this.db.video.update({
                where: { id: video.id },
                data: { size: video.size },
            });
        }
    };

    getVideoSize = (videoPath: string): Promise<number> => {
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

    generateThumbnail = (videoPath: string, thumbnailPath: string, timestamps: string): Promise<void> => {
        return new Promise((resolve, reject) => {
            ffmpeg(videoPath)
                .once("end", () => resolve())
                .once("error", (error) => reject(error))
                .screenshots({
                    timestamps: [timestamps],
                    filename: path.basename(thumbnailPath),
                    folder: path.dirname(thumbnailPath),
                    size: "1920x1080",
                });
        });
    };

    generateSingleThumbnail = async (videoPath: string, videoName: string, login: string) => {
        const duration = await this.getVideoDuration(videoPath); // TODO when is 0
        const thumbnailName = videoName.replace(".mp4", ".jpg");
        const directoryPath = path.resolve(THUMBNAIL_PATH, login);
        if (!fs.existsSync(directoryPath)) {
            fs.mkdirSync(directoryPath, { recursive: true });
        }
        const thumbnailPath = videoPath.replace("videos", "thumbnail").replace(videoName, thumbnailName);

        let timestamp = 300;
        for (let tries = 0; tries < 5; tries++) {
            try {
                await this.generateThumbnail(videoPath, thumbnailPath, this.secondsToTimestamp(timestamp));
                return this.getRelativePath(thumbnailPath);
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

    generateMissingThumbnailsAndUpdate = async () => {
        logger.info("Generate missing thumbnails...");
        // TODO verify the true duration before
        // TODO verify if the thumbnail in video exist, if not generate it
        try {
            const videos = await this.db.video.findMany({
                where: { thumbnail: null, status: Status.DONE },
            });
            const promises = videos.map(async (video) => {
                const thumbnailPath = this.getThumbnailPath(video.displayName, video.filename);
                const videoPath = this.getVideoPath(video.displayName, video.filename);
                const duration = await this.getVideoDuration(videoPath);
                if (!fs.existsSync(path.dirname(thumbnailPath))) {
                    fs.mkdirSync(path.dirname(thumbnailPath), { recursive: true });
                }
                let timestamp = 300;
                if (timestamp >= duration) {
                    timestamp = 30;
                }
                for (let tries = 0; tries < 5; tries++) {
                    try {
                        await this.generateThumbnail(videoPath, thumbnailPath, this.secondsToTimestamp(timestamp));
                        await this.db.video.update({
                            where: {
                                id: video.id,
                            },
                            data: {
                                thumbnail: this.getRelativePath(thumbnailPath),
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
            return this.db.video.findMany({
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

    isVideoCorrupt = (metadata: ffmpeg.FfprobeFormat) => {
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

    fixMalformedVideos = async () => {
        const videos = await this.db.video.findMany({
            where: { status: Status.DONE },
        });
        for (const video of videos) {
            const videoPath = this.getVideoPath(video.displayName, video.filename);
            if (fs.existsSync(videoPath)) {
                try {
                    logger.info(`Processing video: ${videoPath}`);
                    let metadata = await this.getMetadata(videoPath);
                    if (this.isVideoCorrupt(metadata)) {
                        logger.info(`Video might be corrupt. Attempting to fix...`);
                        const fixedVideoPath = videoPath.replace(".mp4", "FIX.mp4");
                        await this.fixVideo(videoPath, fixedVideoPath);
                        metadata = await this.getMetadata(fixedVideoPath);
                        if (this.isVideoCorrupt(metadata)) {
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

    getMaxFrames = (videoPath: string) => {
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

    getMetadata = (videoPath: string): Promise<FfprobeFormat> => {
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

    fixVideo = (inputVideoPath: string, outputVideoPath: string): Promise<void> => {
        return new Promise((resolve, reject) => {
            ffmpeg(inputVideoPath)
                .outputOptions("-vcodec copy")
                .outputOptions("-acodec copy")
                .save(outputVideoPath)
                .once("end", () => resolve())
                .once("error", (error) => reject(error));
        });
    };

    getRelativePath = (fullPath: string): string => {
        let pathParts = fullPath.split(path.sep);
        let relativePath = pathParts.slice(-2).join("/");
        return relativePath;
    };

    secondsToTimestamp = (seconds: number) => {
        const hrs = Math.floor(seconds / 3600);
        const mins = Math.floor((seconds - hrs * 3600) / 60);
        const secs = seconds - hrs * 3600 - mins * 60;
        return `${hrs}:${mins}:${secs}`;
    };

    getVideoDuration = (videoPath: string): Promise<number> => {
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

    updateVideoData = async (filename: string, endAt: Date, thumbnail: string, size: number, duration: number) => {
        return this.db.video.updateMany({
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

    getVideosByChannel = async (broadcaster_id: string): Promise<videoDTO[]> => {
        const videos = await this.db.video.findMany({
            where: {
                broadcasterId: broadcaster_id,
                status: Status.DONE,
            },
            orderBy: { downloadedAt: "desc" },
            select: this.videoSelectConfig,
        });
        const videosWithoutSize = videos.filter((video) => !video.size);
        await Promise.all(videosWithoutSize.map(this.updateVideoSize));
        const transformed = transformVideo(videos);
        return transformed;
    };

    saveVideoInfo = async ({
        userRequesting,
        channel,
        videoName,
        startAt,
        status,
        jobId,
        stream,
        videoQuality,
    }: {
        userRequesting: string[];
        channel: Channel;
        videoName: string;
        startAt: Date;
        status: Status;
        jobId: string;
        stream: StreamDTO;
        videoQuality: Quality;
    }) => {
        try {
            const video = await this.db.video.create({
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
            for (const userId of userRequesting) {
                await this.db.videoRequest.create({
                    data: {
                        video: {
                            connect: {
                                id: video.id,
                            },
                        },
                        user: {
                            connect: {
                                userId: userId,
                            },
                        },
                    },
                });
            }
            for (let title of stream.titles) {
                await this.titleService.createVideoTitle(video.id, title.titleId);
            }
            for (let category of stream.categories) {
                await this.categoryRepository.createVideoCategory(video.id, category.id);
            }
            for (let tag of stream.tags) {
                await this.tagService.addVideoTag(video.id, tag.name);
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

    updateVideoInfo = async (videoName: string, endAt: Date, status: Status) => {
        return this.db.video.update({
            where: {
                filename: videoName,
            },
            data: {
                downloadedAt: endAt,
                status: status,
            },
        });
    };

    getVideoFilePath = (login: string) => {
        const currentDate = DateTime.now().toFormat("ddMMyyyy-HHmmss");
        const filename = `${login}_${currentDate}.mp4`;
        const directoryPath = path.resolve(VIDEO_PATH, login);
        if (!fs.existsSync(directoryPath)) {
            fs.mkdirSync(directoryPath, { recursive: true });
        }
        return path.join(directoryPath, filename);
    };

    setVideoFailed = async () => {
        try {
            await this.db.video.updateMany({
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

    private videoSelectConfig = {
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
}
