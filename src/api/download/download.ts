import { VideoQuality } from "../../models/downloadModel";
import { jobService, tagService } from "../../services";
import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
const logger = rootLogger.child({ domain: "download", service: "downloadService" });
import path from "path";
import { DownloadSchedule, Status } from "@prisma/client";
import { DownloadParams, JobDetail } from "../../types/sharedTypes";
import { transformDownloadSchedule } from "./download.DTO";
import { categoryService } from "../category";
import { channelService } from "../channel";
import { videoService } from "../video";
const os = require("os");
const { create: createYoutubeDl } = require("youtube-dl-exec");

let youtubedl;

if (os.platform() === "win32") {
    youtubedl = createYoutubeDl("bin/yt.exe");
} else if (os.platform() === "linux") {
    youtubedl = createYoutubeDl("bin/yt-dlp");
}

// TODO
export const planningRecord = async (userId: string) => {
    const channel = await channelService.getChannelDetailDB(userId);
    return "Successful registration planning";
};

export const getAllTagsFromStream = async (streamId) => {
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

export const getAllCategoriesFromStream = async (streamId) => {
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

export const getAllTitlesFromStream = async (streamId) => {
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

export const startDownload = async ({
    requestingUserId,
    channel,
    jobId,
    stream,
    videoQuality,
}: DownloadParams) => {
    const videoFilePath = videoService.getVideoFilePath(channel.broadcasterLogin);
    const cookiesFilePath = path.resolve(process.env.DATA_DIR, "cookies.txt");
    const filename = path.basename(videoFilePath);
    await videoService.saveVideoInfo({
        userRequesting: requestingUserId,
        channel: channel,
        videoName: filename,
        startAt: new Date(),
        status: Status.PENDING,
        jobId: jobId,
        stream: stream,
        videoQuality: videoQuality,
    });
    const resolution: string = videoService.mapQualityToVideoQuality(videoQuality);
    return new Promise<string>((resolve, reject) => {
        logger.info(
            `Download: ${JSON.stringify({
                download: `https://www.twitch.tv/${channel.broadcasterLogin}`,
                format: `best[height=${resolution}]`,
                output: videoFilePath,
                cookies: cookiesFilePath,
            })} `
        );
        const subprocess = youtubedl.exec(`https://www.twitch.tv/${channel.broadcasterLogin}`, {
            format: `best[height=${resolution}]`,
            output: videoFilePath,
        });

        // subprocess.stdout.on("data", (chunk) => {
        //     logger.info(`STDOUT: ${chunk.toString()}`);
        // });

        subprocess.stderr.on("data", (chunk) => {
            const message = chunk.toString();
            if (
                message.includes("error") ||
                message.includes("error") ||
                (!message.includes("Skip") && !message.includes("Opening") && !message.includes("frame"))
            ) {
                logger.error(`STDERR: ${message}`);
            }
            // else {
            //     logger.info(`STDOUT: ${message}`);
            // }
        });

        subprocess.on("close", async (code) => {
            if (code !== 0) {
                reject(new Error(`youtube-dl process exited with code ${code}`));
            } else {
                await finishDownload(videoFilePath, filename, channel.broadcasterLogin);
                resolve(videoFilePath);
            }
        });
    });
};

export const finishDownload = async (videoPath: string, filename: string, login: string) => {
    const endAt = new Date();
    let duration, thumbnailPath, size;

    try {
        duration = await videoService.getVideoDuration(videoPath);
    } catch (error) {
        logger.error("Error getting video duration:", error);
    }

    try {
        thumbnailPath = await videoService.generateSingleThumbnail(videoPath, filename, login);
    } catch (error) {
        logger.error("Error generating thumbnail:", error);
    }

    try {
        size = await videoService.getVideoSize(videoPath);
    } catch (error) {
        logger.error("Error getting video size:", error);
    }

    try {
        await videoService.updateVideoData(filename, endAt, thumbnailPath, size, duration);
    } catch (error) {
        logger.error("Error updating video data:", error);
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

export const addSchedule = async (newSchedule, userId) => {
    try {
        const transformedScheduleData = await transformDownloadSchedule(newSchedule, userId);
        const createdDownloadSchedule = await prisma.downloadSchedule.create({
            data: transformedScheduleData.downloadSchedule,
        });

        if (transformedScheduleData.tags.length > 0) {
            await tagService.addAllDownloadScheduleTags(
                transformedScheduleData.tags.map((tag) => ({ tagId: tag.name })),
                createdDownloadSchedule.id
            );
        }

        if (transformedScheduleData.category) {
            await categoryService.addDownloadScheduleCategory(
                createdDownloadSchedule.id,
                transformedScheduleData.category.id
            );
        }
    } catch (error) {
        if (error.code === "P2002") {
            throw new Error("User is already assigned to this broadcaster ID");
        }
        throw error;
    }
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

const getScheduleDetail = async (schedule, broadcaster_id: string) => {
    // TODO: A modifier
    const loginId = schedule.requested_by;
    const user = await channelService.getChannelDetailDB(broadcaster_id);
    const jobId = jobService.createJobId();
    const quality = VideoQuality[schedule.quality as keyof typeof VideoQuality] || VideoQuality.MEDIUM;
    return { loginId, user, jobId, quality };
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

const getScheduleByFollowedChannel = async (broadcaster_id: string): Promise<DownloadSchedule | null> => {
    return prisma.downloadSchedule.findFirst({
        where: {
            provider: Provider.FOLLOWED_CHANNEL,
            channel: {
                usersFollowing: {
                    some: {
                        broadcasterId: broadcaster_id,
                    },
                },
            },
        },
    });
};

const getAllScheduleByChannel = async (broadcasterId: string): Promise<DownloadSchedule[]> => {
    return await prisma.downloadSchedule.findMany({
        where: {
            broadcasterId: broadcasterId,
        },
    });
};
