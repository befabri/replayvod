import { getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import { Stream, User } from "../models/twitchModel";
import { Video } from "../models/videoModel";
import { DownloadSchedule, VideoQuality } from "../models/downloadModel";
import { youtubedlLogger } from "../middlewares/loggerMiddleware";
import { userService, videoService } from "../services";
import fs from "fs";
import path from "path";
import moment from "moment";
const os = require("os");
const { create: createYoutubeDl } = require("youtube-dl-exec");
let youtubedl;
if (os.platform() === "win32") {
    youtubedl = createYoutubeDl("bin/yt.exe");
} else if (os.platform() === "linux") {
    youtubedl = createYoutubeDl("bin/yt-dlp");
}

const twitchAPI = new TwitchAPI();

export const planningRecord = async (userId: string) => {
    const user = await userService.getUserDetailDB(userId);
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
    const gamesCollection = db.collection("games");

    const gameData = await gamesCollection.findOne({ id: stream.game_id });
    let gameDetail = [{ id: stream.game_id, name: "" }];

    if (gameData) {
        gameDetail[0].name = gameData.name;
    }

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
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    return videoCollection.updateOne(
        { filename: videoName },
        {
            $set: {
                downloaded_at: endAt,
                status: status,
            },
        }
    );
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
        youtubedlLogger.info(
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
            youtubedlLogger.info(`STDOUT: ${chunk.toString()}`);
        });

        subprocess.stderr.on("data", (chunk) => {
            const message = chunk.toString();
            if (
                message.includes("error") ||
                message.includes("error") ||
                (!message.includes("Skip") && !message.includes("Opening") && !message.includes("frame"))
            ) {
                youtubedlLogger.error(`STDERR: ${message}`);
            } else {
                youtubedlLogger.info(`STDOUT: ${message}`);
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
        console.error("Error getting video duration:", error);
    }

    try {
        thumbnailPath = await videoService.generateSingleThumbnail(videoPath, filename, login);
    } catch (error) {
        console.error("Error generating thumbnail:", error);
    }

    try {
        size = await videoService.getVideoSize(videoPath);
    } catch (error) {
        console.error("Error getting video size:", error);
    }

    try {
        await videoService.updateVideoData(filename, endAt, thumbnailPath, size, duration);
    } catch (error) {
        console.error("Error updating video data:", error);
    }
};

export const findPendingJob = async (broadcaster_id: string) => {
    const db = await getDbInstance();
    const jobCollection = db.collection("videos");

    return jobCollection.findOne({ broadcaster_id, status: "Pending" });
};

export const setVideoFailed = async (jobId: string) => {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");
    const endAt = new Date();
    return videoCollection.updateOne(
        { job_id: jobId },
        {
            $set: {
                downloaded_at: endAt,
                status: "Failed",
            },
        }
    );
};

export const updateVideoCollection = async (user_id: string) => {
    const db = await getDbInstance();
    const videoCollection = db.collection("videos");

    const stream = await twitchAPI.getStreamByUserId(user_id);
    const videoData = await videoCollection.findOne({ broadcaster_id: user_id });

    if (videoData) {
        if (!videoData.category.some((category: { id: string; name: string }) => category.id === stream.game_id)) {
            videoData.category.push({ id: stream.game_id, name: stream.game_name });
        }

        if (!videoData.title.includes(stream.title)) {
            videoData.title.push(stream.title);
        }

        if (!videoData.tags.some((tag: string) => stream.tags.includes(tag))) {
            videoData.tags.push(...stream.tags);
        }

        if (stream.viewer_count > videoData.viewer_count) {
            videoData.viewer_count = stream.viewer_count;
        }

        return videoCollection.updateOne({ broadcaster_id: user_id }, { $set: videoData });
    } else {
        throw new Error("No video data found for the provided user_id.");
    }
};

export const insertSchedule = async (data: DownloadSchedule) => {
    const db = await getDbInstance();
    const scheduleCollection = db.collection("schedule");
    await scheduleCollection.insertOne(data);
};

export default {
    planningRecord,
    saveVideoInfo,
    updateVideoInfo,
    getVideoFilePath,
    startDownload,
    finishDownload,
    findPendingJob,
    setVideoFailed,
    updateVideoCollection,
    insertSchedule,
};
