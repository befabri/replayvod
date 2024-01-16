import { FallbackResolutions, Resolution } from "../../models/downloadModel";
import { jobService } from "../../services";
import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import path from "path";
import { Status } from "@prisma/client";
import { DownloadParams, JobDetail } from "../../types/sharedTypes";
import { spawn } from "child_process";
import { platform } from "os";
import fs from "fs/promises";
import { YtdlExecFunction, create as createYoutubeDl } from "youtube-dl-exec";
import { videoFeature } from "../video";
const logger = rootLogger.child({ domain: "download", service: "downloadService" });

let youtubedl: {
    exec: YtdlExecFunction;
};

if (platform() === "win32") {
    youtubedl = createYoutubeDl("bin/yt.exe");
} else if (platform() === "linux") {
    youtubedl = createYoutubeDl("bin/yt-dlp");
}

const getYoutubeDlBinary = () => {
    if (platform() === "win32") {
        return "bin/yt.exe";
    } else if (platform() === "linux") {
        return "bin/yt-dlp";
    } else {
        throw new Error("Unsupported OS platform.");
    }
};

export const getAllTagsFromStream = async (streamId: string) => {
    const streamTags = await prisma.streamTag.findMany({
        where: {
            streamId: streamId,
        },
        include: {
            tag: true,
        },
    });

    return streamTags.map((st) => st.tag.name);
};

export const getAllCategoriesFromStream = async (streamId: string) => {
    const streamCategories = await prisma.streamCategory.findMany({
        where: {
            streamId: streamId,
        },
        include: {
            category: true,
        },
    });

    return streamCategories.map((sc) => sc.category.name);
};

export const getAllTitlesFromStream = async (streamId: string) => {
    const streamTitles = await prisma.streamTitle.findMany({
        where: {
            streamId: streamId,
        },
        include: {
            title: true,
        },
    });

    return streamTitles.map((st) => st.title.name);
};

const getAvailableResolutions = (url: string): Promise<Resolution[]> => {
    return new Promise((resolve, reject) => {
        const formatsData: string[] = [];
        const binaryPath = getYoutubeDlBinary();
        const ytProcess = spawn(binaryPath, ["--list-formats", url]);

        ytProcess.stdout.on("data", (data) => {
            formatsData.push(data.toString());
        });

        ytProcess.stderr.on("data", (data) => {
            logger.error(data);
        });

        ytProcess.on("close", (code) => {
            if (code !== 0) {
                reject(new Error(`Failed to list formats with youtube-dl, exited with code ${code}`));
            } else {
                const allMatches = formatsData.join("").matchAll(/(\d{3,4})p/g);
                const uniqueResolutions: Resolution[] = [
                    ...new Set(Array.from(allMatches, (m) => m[1] as Resolution)),
                ];
                resolve(uniqueResolutions);
            }
        });
    });
};

const selectBestResolution = async (preferredResolution: Resolution, url: string): Promise<Resolution> => {
    const availableResolutions = await getAvailableResolutions(url);
    if (availableResolutions.includes(preferredResolution)) {
        return preferredResolution;
    }
    const resolutionPreferences: FallbackResolutions = {
        "1080": ["720", "480"],
        "720": ["480", "360"],
        "480": ["360", "160"],
        "360": [],
        "160": [],
    };
    for (let fallbackResolution of resolutionPreferences[preferredResolution]) {
        if (availableResolutions.includes(fallbackResolution)) {
            return fallbackResolution;
        }
    }
    throw new Error("No suitable resolution found.");
};

async function deleteFile(filePath: string): Promise<void> {
    try {
        await fs.unlink(filePath);
    } catch (error) {
        const e = error as NodeJS.ErrnoException;
        if (e.code === "ENOENT") {
            throw new Error(`File not found at ${filePath}`);
        } else {
            throw new Error(`Error deleting file at ${filePath}: ${e.message}`);
        }
    }
}

async function runYoutubeDL(
    broadcasterLogin: string,
    resolution: Resolution,
    tmpVideoFilePath: string
): Promise<void> {
    return new Promise((resolve, reject) => {
        const subprocess = youtubedl.exec(`https://www.twitch.tv/${broadcasterLogin}`, {
            format: `best[height=${resolution}]`,
            output: tmpVideoFilePath,
            fixup: "never",
        });

        subprocess.stderr?.on("data", (chunk) => {
            const message = chunk.toString();
            if (message.includes("frame") && message.includes("fps")) {
                logger.info(message);
            }
            if (message.includes("ERROR") || message.includes("error") || message.includes("Error")) {
                logger.error(message);
            }
        });

        subprocess.on("close", (code) => {
            if (code !== 0) {
                reject(
                    new Error(
                        `youtube-dl process (for broadcasterLogin: ${broadcasterLogin}) exited with code ${code}`
                    )
                );
            } else {
                resolve();
            }
        });
    });
}

async function runFFMPEG(tmpVideoFilePath: string, videoFilePath: string, aRate: number): Promise<void> {
    return new Promise((resolve, reject) => {
        const ffmpegArgs = ["-i", tmpVideoFilePath, "-c:v", "copy", "-af", `asetrate=${aRate}`, videoFilePath];
        const ffmpegProcess = spawn("ffmpeg", ffmpegArgs);

        ffmpegProcess.stderr.on("data", (data) => {
            logger.error(`FFMPEG: ${data}`);
        });

        ffmpegProcess.on("close", (code) => {
            if (code !== 0) {
                reject(new Error(`ffmpeg process (for video: ${tmpVideoFilePath}) exited with code ${code}`));
            } else {
                resolve();
            }
        });
    });
}

const proceedWithDownload = async (
    broadcasterLogin: string,
    filename: string,
    resolution: Resolution,
    tmpVideoFilePath: string,
    videoFilePath: string,
    aRate: number
) => {
    logger.info(
        `Download: ${JSON.stringify({
            download: `https://www.twitch.tv/${broadcasterLogin}`,
            format: `best[height=${resolution}]`,
            output: videoFilePath,
            // cookies: cookiesFilePath,
        })} `
    );
    try {
        await runYoutubeDL(broadcasterLogin, resolution, tmpVideoFilePath);
        await runFFMPEG(tmpVideoFilePath, videoFilePath, aRate);
        await deleteFile(tmpVideoFilePath);
        await completeVideoProcessing(videoFilePath, filename, broadcasterLogin);
        return videoFilePath;
    } catch (error) {
        await deleteFile(tmpVideoFilePath);
        if (
            error instanceof Error &&
            error.message !== `youtube-dl process (for broadcasterLogin: ${broadcasterLogin}) exited with code 0`
        ) {
            await deleteFile(videoFilePath);
        }
        throw error;
    }
};

export const startDownload = async ({
    requestingUserId,
    channel,
    jobId,
    stream,
    videoQuality,
}: DownloadParams) => {
    const videoFilePath = videoFeature.getVideoFilePath(channel.broadcasterLogin);
    // const cookiesFilePath = path.resolve(process.env.DATA_DIR, "cookies.txt");
    const filename = path.basename(videoFilePath);
    await videoFeature.saveVideoInfo({
        userRequesting: requestingUserId,
        channel: channel,
        videoName: filename,
        startAt: new Date(),
        status: Status.PENDING,
        jobId: jobId,
        stream: stream,
        videoQuality: videoQuality,
    });
    const resolution = videoFeature.mapQualityToVideoQuality(videoQuality);
    const aRate = 48000;
    const tmpVideoFilePath = videoFilePath.replace(".mp4", "_tmp.mp4");
    try {
        const resolutionToUse = await selectBestResolution(
            resolution,
            `https://www.twitch.tv/${channel.broadcasterLogin}`
        );
        const result = await proceedWithDownload(
            channel.broadcasterLogin,
            filename,
            resolutionToUse,
            tmpVideoFilePath,
            videoFilePath,
            aRate
        );
        return result;
    } catch (error) {
        logger.error(error);
        throw error;
    }
};

// TODO: make it better
export const completeVideoProcessing = async (videoPath: string, filename: string, login: string) => {
    const endAt = new Date();
    let duration, thumbnailPath, size;
    try {
        duration = await videoFeature.getVideoDuration(videoPath);
    } catch (error) {
        logger.error("Error getting video duration: %s", error);
    }

    try {
        thumbnailPath = await videoFeature.generateSingleThumbnail(videoPath, filename, login);
    } catch (error) {
        logger.error("Error generating thumbnail: %s", error);
    }

    try {
        size = await videoFeature.getVideoSize(videoPath);
    } catch (error) {
        logger.error("Error getting video size: %s", error);
    }

    try {
        if (!thumbnailPath || thumbnailPath === undefined) {
            thumbnailPath = "";
        }
        if (!size || size === undefined) {
            size = 0;
        }
        if (!duration || duration === undefined) {
            duration = 0;
        }
        await videoFeature.updateVideoData(filename, endAt, thumbnailPath, size, duration);
    } catch (error) {
        logger.error("Error updating video data: %s", error);
    }
};

export const setVideoFailed = async (jobId: string) => {
    const endAt = new Date();
    return await prisma.video.update({
        where: { jobId: jobId },
        data: {
            downloadedAt: endAt,
            status: Status.FAILED,
        },
    });
};

export const updateVideoCollection = async (user_id: string) => {
    // TODO: Implémenter cette fonction en sachant que elle est fausse puisque il faut pouvoir identifier
    // la vidéo au stream actuelle et non update toutes les vidéos basé sur un broadcasterId
    //
    // try {
    //     const stream = await twitchService.getStreamByUserId(user_id);
    //     const videoData = await prisma.video.findUnique({
    //         where: { broadcasterId: user_id },
    //     });
    //     if (!videoData) {
    //         throw new Error("No video data found for the provided user_id.");
    //     }
    //     // Handle category update
    //     if (!videoData.VideoCategory.some((category) => category.id === stream.game_id)) {
    //         await prisma.videoCategory.create({
    //             data: {
    //                 videoId: videoData.id,
    //                 categoryId: stream.game_id,
    //             },
    //         });
    //     }
    //     // Handle title update
    //     const existingTitle = await prisma.title.findUnique({
    //         where: { name: stream.title },
    //     });
    //     if (!existingTitle) {
    //         const newTitle = await prisma.title.create({
    //             data: { name: stream.title },
    //         });
    //         await prisma.videoTitle.create({
    //             data: {
    //                 videoId: videoData.id,
    //                 titleId: newTitle.id,
    //             },
    //         });
    //     }
    //     // Handle tags update
    //     for (const tag of stream.tags) {
    //         const existingTag = await prisma.tag.findUnique({
    //             where: { name: tag },
    //         });
    //         if (!existingTag) {
    //             const newTag = await prisma.tag.create({
    //                 data: { name: tag },
    //             });
    //             await prisma.videoTag.create({
    //                 data: {
    //                     videoId: videoData.id,
    //                     tagId: newTag.name,
    //                 },
    //             });
    //         }
    //     }
    //     // Handle viewer count update
    //     if (stream.viewer_count > videoData.viewerCount) {
    //         await prisma.video.update({
    //             where: { id: videoData.id },
    //             data: { viewerCount: stream.viewer_count },
    //         });
    //     }
    // } catch (error) {
    //     throw error;
    // }
    return;
};

export const handleDownload = async (
    { stream, userId, channel, jobId, quality }: JobDetail,
    broadcasterId: string
) => {
    const pendingJob = await jobService.findPendingJobByBroadcasterId(broadcasterId);
    if (pendingJob) {
        return;
    }
    try {
        jobService.createJob(jobId, async () => {
            try {
                await startDownload({
                    requestingUserId: userId,
                    channel: channel,
                    jobId: jobId,
                    stream: stream,
                    videoQuality: quality,
                });
            } catch (error) {
                logger.error("Error when downloading: %s", error);
                throw error;
            }
        });
    } catch (error) {
        logger.error("Failed to create job: %s", error);
        throw error;
    }
};

export const downloadSchedule = async (broadcaster_id: string) => {
    // Todo: A savoir que plusieurs utilisateurs peuvent avoir la même video demandé
    // et donc il faut modifié jobDetail + handleDownload
    // let schedule;
    // schedule = await getScheduleByFollowedChannel(broadcaster_id);
    // if (schedule) {
    //     const jobDetails = await getScheduleDetail(schedule, broadcaster_id);
    //     await handleDownload(jobDetails, broadcaster_id);
    // } else {
    //     schedule = getAllScheduleByChannel;
    //     // const jobDetails = await getScheduleDetail(schedule, broadcaster_id);
    //     // await handleDownload(jobDetails, broadcaster_id);
    // }
};
