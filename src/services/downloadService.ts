import TwitchAPI from "../utils/twitchAPI";
import { Stream, User } from "../models/twitchModel";
import { VideoQuality } from "../models/downloadModel";
import { channelService, videoService, jobService, categoryService } from "../services";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "downloadService" });
import fs from "fs";
import path from "path";
import moment from "moment";
import { DownloadSchedule, Provider, Quality, Status, Trigger } from "@prisma/client";
const os = require("os");
const { create: createYoutubeDl } = require("youtube-dl-exec");

let youtubedl;

if (os.platform() === "win32") {
    youtubedl = createYoutubeDl("bin/yt.exe");
} else if (os.platform() === "linux") {
    youtubedl = createYoutubeDl("bin/yt-dlp");
}

const twitchAPI = new TwitchAPI();

// TODO
export const planningRecord = async (userId: string) => {
    const channel = await channelService.getChannelDetailDB(userId);
    return "Successful registration planning";
};

export const saveVideoInfo = async (
    userRequesting: string,
    broadcasterId: string,
    displayName: string,
    videoName: string,
    startAt: Date,
    status: string,
    jobId: string,
    stream: Stream,
    videoQuality: VideoQuality
) => {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const categoryData = await categoryService.getCategoryById(stream.game_id); // Add category if not exist
    const categoryDetail = [
        {
            id: stream.game_id,
            name: categoryData ? categoryData.name : "",
        },
    ];

    const videoData: Video = {
        id: stream.id,
        filename: videoName,
        status: status,
        display_name: displayName,
        broadcaster_id: broadcasterId,
        requested_by: userRequesting,
        start_download_at: startAt,
        downloaded_at: "",
        job_id: jobId,
        category: gameDetail,
        title: [stream.title],
        tags: stream.tags,
        viewer_count: stream.viewer_count,
        language: stream.language,
        quality: videoQuality,
    };

    return videoCollection.insertOne(videoData);
};

export const updateVideoInfo = async (videoName: string, endAt: Date, status: string) => {
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
    const currentDate = moment().format("DDMMYYYY-HHmmss");
    const filename = `${login}_${currentDate}.mp4`;
    const directoryPath = path.resolve(process.env.PUBLIC_DIR, "videos", login);
    if (!fs.existsSync(directoryPath)) {
        fs.mkdirSync(directoryPath, { recursive: true });
    }
    return path.join(directoryPath, filename);
};

export const startDownload = async (
    requestingUserId: string,
    user: User,
    jobId: string,
    stream: Stream,
    videoQuality: VideoQuality
) => {
    const videoFilePath = getVideoFilePath(user.login);
    const cookiesFilePath = path.resolve(process.env.DATA_DIR, "cookies.txt");
    const startAt = new Date();
    const filename = path.basename(videoFilePath);
    await saveVideoInfo(
        requestingUserId,
        user.id,
        user.display_name,
        filename,
        startAt,
        "Pending",
        jobId,
        stream,
        videoQuality
    );

    return new Promise<string>((resolve, reject) => {
        logger.info(
            `Download: ${JSON.stringify({
                download: `https://www.twitch.tv/${user.login}`,
                format: `best[height=${videoQuality}]`,
                output: videoFilePath,
                cookies: cookiesFilePath,
            })} `
        );
        const subprocess = youtubedl.exec(`https://www.twitch.tv/${user.login}`, {
            format: `best[height=${videoQuality}]`,
            output: videoFilePath,
        });

        subprocess.stdout.on("data", (chunk) => {
            logger.info(`STDOUT: ${chunk.toString()}`);
        });

        subprocess.stderr.on("data", (chunk) => {
            const message = chunk.toString();
            if (
                message.includes("error") ||
                message.includes("error") ||
                (!message.includes("Skip") && !message.includes("Opening") && !message.includes("frame"))
            ) {
                logger.error(`STDERR: ${message}`);
            } else {
                logger.info(`STDOUT: ${message}`);
            }
        });

        subprocess.on("close", async (code) => {
            if (code !== 0) {
                reject(new Error(`youtube-dl process exited with code ${code}`));
            } else {
                await finishDownload(videoFilePath, filename, user.login);
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
    //     const stream = await twitchAPI.getStreamByUserId(user_id);
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

const isValidProvider = (provider: string): provider is Provider => {
    return Object.values(Provider).includes(provider as Provider);
};

const isValidTrigger = (trigger: string): trigger is Trigger => {
    return Object.values(Trigger).includes(trigger as Trigger);
};

export const addSchedule = async (scheduleData) => {
    if (!isValidProvider(scheduleData.provider) || !isValidTrigger(scheduleData.trigger)) {
        throw new Error("Invalid provider or trigger value");
    }
    const timeBeforeDeleteDate = new Date();
    timeBeforeDeleteDate.setMinutes(timeBeforeDeleteDate.getMinutes() + scheduleData.timeBeforeDelete);
    const broadcasterId = await channelService.getChannelBroadcasterIdByName(scheduleData.channelName);
    if (!broadcasterId) {
        throw new Error("ChannelName dont exist");
    }
    return await prisma.downloadSchedule.create({
        data: {
            provider: scheduleData.provider as Provider,
            broadcasterId: broadcasterId,
            viewersCount: scheduleData.viewersCount,
            timeBeforeDelete: timeBeforeDeleteDate,
            trigger: scheduleData.trigger as Trigger,
            quality: scheduleData.quality as Quality,
            isDeleteRediff: scheduleData.isDeleteRediff,
            requestedBy: scheduleData.requested_by,
        },
    });
};

export const handleDownload = async (jobDetails: any, broadcaster_id: string) => {
    // TODO: A modifier
    const pendingJob = await jobService.findPendingJob(broadcaster_id);
    if (pendingJob) {
        return;
    }
    const stream = await twitchAPI.getStreamByUserId(broadcaster_id);
    if (stream === null) {
        return;
    }

    jobService.createJob(jobDetails.jobId, async () => {
        try {
            const video = await startDownload(
                jobDetails.loginId,
                jobDetails.user,
                jobDetails.jobId,
                stream,
                jobDetails.quality
            );
        } catch (error) {
            logger.error("Error when downloading:", error);
            throw error;
        }
    });
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
    let schedule;
    schedule = await getScheduleByFollowedChannel(broadcaster_id);
    if (schedule) {
        const jobDetails = await getScheduleDetail(schedule, broadcaster_id);
        await handleDownload(jobDetails, broadcaster_id);
    } else {
        schedule = getAllScheduleByChannel;
        // const jobDetails = await getScheduleDetail(schedule, broadcaster_id);
        // await handleDownload(jobDetails, broadcaster_id);
    }
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

export default {
    planningRecord,
    saveVideoInfo,
    updateVideoInfo,
    getVideoFilePath,
    startDownload,
    finishDownload,
    setVideoFailed,
    updateVideoCollection,
    addSchedule,
    handleDownload,
};
