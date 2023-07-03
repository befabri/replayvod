import { getDbInstance } from "../models/db";
import fs from "fs";
import path from "path";
import { Collection, Document, WithId } from "mongodb";
import ffmpeg from "fluent-ffmpeg";
import { Video } from "../models/videoModel";
import { fixvideosLogger, logger } from "../middlewares/loggerMiddleware";

class VideoService {
    async getVideoById(id: string): Promise<Video | null> {
        const db = await getDbInstance();
        const videoCollection: Collection<Video> = db.collection("videos");
        return videoCollection.findOne({ id: id });
    }

    async getFinishedVideosFromUser(userId: string) {
        const db = await getDbInstance();
        const videoCollection = db.collection("videos");
        const videos = await videoCollection.find({ requested_by: userId, status: "Finished" }).toArray();
        const updatePromises = videos.map((video) => {
            if (!video.size) {
                return this.updateVideoSize(video, videoCollection);
            }
        });
        await Promise.all(updatePromises);
        return videos;
    }

    updateVideoSize(video: WithId<Document>, videoCollection: Collection<Document>) {
        return new Promise((resolve, reject) => {
            const filePath = path.resolve(
                process.env.PUBLIC_DIR,
                "videos",
                video.display_name.toLowerCase(),
                video.filename
            );
            if (fs.existsSync(filePath)) {
                const stat = fs.statSync(filePath);
                const fileSizeInBytes = stat.size;
                const fileSizeInMegabytes = fileSizeInBytes / (1024 * 1024);
                video.size = `${fileSizeInMegabytes.toFixed(2)} MB`;
                videoCollection
                    .updateOne({ _id: video._id }, { $set: { size: video.size } })
                    .then(() => resolve(undefined))
                    .catch((err) => reject(err));
            } else {
                resolve(undefined);
            }
        });
    }

    getVideoSize(videoPath: string): Promise<number> {
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
    }

    generateThumbnail(videoPath: string, thumbnailPath: string, timestamps: string): Promise<void> {
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
    }

    async generateSingleThumbnail(videoPath: string, videoName: string, login: string) {
        const duration = await this.getVideoDuration(videoPath);
        const thumbnailName = videoName.replace(".mp4", ".jpg");
        const directoryPath = path.resolve(process.env.PUBLIC_DIR, "thumbnail", login);
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
    }

    async generateMissingThumbnailsAndUpdate() {
        // TODO verify the true duration before
        try {
            const db = await getDbInstance();
            const videoCollection = db.collection("videos");
            const videos = await videoCollection.find({ thumbnail: null, status: "Finished" }).toArray();
            const promises = videos.map(async (video) => {
                const thumbnailPath = path.resolve(
                    process.env.PUBLIC_DIR,
                    "thumbnail",
                    video.display_name.toLowerCase(),
                    video.filename.replace(".mp4", ".jpg")
                );
                const videoPath = path.resolve(
                    process.env.PUBLIC_DIR,
                    "videos",
                    video.display_name.toLowerCase(),
                    video.filename
                );
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
                        await videoCollection.updateOne(
                            { _id: video._id },
                            { $set: { thumbnail: this.getRelativePath(thumbnailPath) } }
                        );
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
            return videoCollection.find({ thumbnail: { $ne: null } }).toArray();
        } catch (error) {
            logger.error("Error generating missing thumbnails and updating collection:", error);
            return [];
        }
    }

    isVideoCorrupt(metadata) {
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
    }

    async fixMalformedVideos() {
        const db = await getDbInstance();
        const videoCollection = db.collection("videos");
        const videos = await videoCollection.find().toArray();
        for (const video of videos) {
            const videoPath = path.resolve(
                process.env.PUBLIC_DIR,
                "videos",
                video.display_name.toLowerCase(),
                video.filename
            );
            if (fs.existsSync(videoPath)) {
                try {
                    fixvideosLogger.info(`Processing video: ${videoPath}`);
                    let metadata = await this.getMetadata(videoPath);
                    if (this.isVideoCorrupt(metadata)) {
                        fixvideosLogger.info(`Video might be corrupt. Attempting to fix...`);
                        const fixedVideoPath = videoPath.replace(".mp4", "FIX.mp4");
                        await this.fixVideo(videoPath, fixedVideoPath);
                        metadata = await this.getMetadata(fixedVideoPath);
                        if (this.isVideoCorrupt(metadata)) {
                            fixvideosLogger.error(`Video is still corrupt after fixing.`);
                        } else {
                            fixvideosLogger.info(`Video has been successfully fixed.`);
                            const tempOriginalPath = videoPath.replace(".mp4", "TEMP.mp4");
                            fs.renameSync(videoPath, tempOriginalPath);
                            fs.renameSync(fixedVideoPath, videoPath);
                            fs.unlinkSync(tempOriginalPath);
                            fixvideosLogger.info(`Successfully replaced the corrupt video with the fixed one.`);
                        }
                    } else {
                        fixvideosLogger.info(`Video seems fine, no actions taken.`);
                    }
                } catch (error) {
                    fixvideosLogger.error(`Error processing video at path ${videoPath}: ${error.message}`);
                }
            } else {
                fixvideosLogger.warn(`Video does not exist at path: ${videoPath}`);
            }
        }
    }

    getMaxFrames(videoPath: string): Promise<number> {
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
    }

    getMetadata(videoPath) {
        return new Promise((resolve, reject) => {
            ffmpeg.ffprobe(videoPath, (err, metadata) => {
                if (err) {
                    reject(err);
                } else {
                    resolve(metadata);
                }
            });
        });
    }

    fixVideo(inputVideoPath: string, outputVideoPath: string): Promise<void> {
        return new Promise((resolve, reject) => {
            ffmpeg(inputVideoPath)
                .outputOptions("-vcodec copy")
                .outputOptions("-acodec copy")
                .save(outputVideoPath)
                .on("end", resolve)
                .on("error", reject);
        });
    }

    getRelativePath(fullPath: string): string {
        let pathParts = fullPath.split(path.sep);
        let relativePath = pathParts.slice(-2).join("/");
        return relativePath;
    }

    secondsToTimestamp(seconds: number) {
        const hrs = Math.floor(seconds / 3600);
        const mins = Math.floor((seconds - hrs * 3600) / 60);
        const secs = seconds - hrs * 3600 - mins * 60;
        return `${hrs}:${mins}:${secs}`;
    }

    getVideoDuration(videoPath: string): Promise<number> {
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
    }

    async getVideosFromUser(userId: string) {
        const db = await getDbInstance();
        const videoCollection = db.collection("videos");
        const videos = await videoCollection.find({ requested_by: userId }).toArray();
        const updatePromises = videos.map((video) => {
            if (!video.size) {
                return this.updateVideoSize(video, videoCollection);
            }
        });
        await Promise.all(updatePromises);
        return videos;
    }

    async updateVideoData(filename: string, endAt: Date, thumbnail: string, size: number, duration: number) {
        const db = await getDbInstance();
        const videoCollection = db.collection("videos");

        return videoCollection.updateOne(
            { filename: filename },
            {
                $set: {
                    downloaded_at: endAt,
                    status: "Finished",
                    thumbnail: thumbnail,
                    size: size,
                    duration: duration,
                },
            }
        );
    }

    async getVideosByUser(userId: string) {
        const db = await getDbInstance();
        const videoCollection = db.collection("videos");
        const videos = videoCollection.find({ broadcaster_id: userId, status: "Finished" }).toArray();
        return videos;
    }
}

export default VideoService;
