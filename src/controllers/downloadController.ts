import { Request, Response } from "express";
import { userService, downloadService, jobService } from "../services";
import { User } from "../models/twitchModel";
import TwitchAPI from "../utils/twitchAPI";
import { DownloadSchedule, VideoQuality } from "../models/downloadModel";
import { youtubedlLogger } from "../middlewares/loggerMiddleware";

const CALLBACK_URL_WEBHOOK = process.env.CALLBACK_URL_WEBHOOK;
const twitchAPI = new TwitchAPI();

export const scheduleUser = async (req: Request, res: Response) => {
    if (!req.session?.passport?.user) {
        res.status(401).send("Unauthorized");
        return;
    }
    const userId = req.params.id;

    if (!userId || typeof userId !== "string" || userId == undefined) {
        res.status(400).send("Invalid user id");
        return;
    }

    try {
        const result = await downloadService.planningRecord(userId);
        res.json(result);
    } catch (error) {
        console.error("Error recording user:", error);
        res.status(500).send("Error recording user");
    }
};

export const scheduleDownload = async (req: Request, res: Response) => {
    if (!req.session?.passport?.user) {
        res.status(401).send("Unauthorized");
        return;
    }
    const data: DownloadSchedule = req.body;
    if (!data.source || !data.channelName || !data.trigger || !data.quality) {
        res.status(400).send("Invalid request data");
        return;
    }
    data.requested_by = req.session.passport.user.data[0].id;
    const user = await userService.getUserDetailByName(data.channelName);

    if (!user) {
        res.status(400).send("Invalid request data");
        return;
    }
    try {
        await downloadService.insertSchedule(data);
        res.status(200).send("Schedule saved successfully.");
    } catch (error) {
        console.error("Error scheduling download:", error);
        res.status(500).send("Error scheduling download.");
    }
};

export const downloadStream = async (req: Request, res: Response) => {
    if (!req.session?.passport?.user) {
        res.status(401).send("Unauthorized");
        return;
    }
    const broadcasterId = req.params.id;
    const quality = VideoQuality[req.params.quality as keyof typeof VideoQuality] || VideoQuality.MEDIUM;
    const user = (await userService.getUserDetailDB(broadcasterId)) as User;
    if (!user) {
        res.status(404).send("User not found");
        return;
    }
    const stream = await twitchAPI.getStreamByUserId(broadcasterId);
    if (stream === null) {
        res.status(400).json({ message: "Stream is offline" });
        return;
    }
    const loginId = req.session.passport.user.data[0].id;
    const pendingJob = await jobService.findPendingJob(broadcasterId);
    if (pendingJob) {
        res.status(400).json({
            message: "There is already a job running for this broadcaster.",
            jobId: pendingJob.id,
        });
        return;
    }
    const jobId = jobService.createJobId();
    await downloadService.handleDownload({ loginId, user, jobId, quality }, broadcasterId);
    res.json({ jobId });
};

export const getJobStatus = async (req: Request, res: Response) => {
    const jobId = req.params.id;
    const status = jobService.getJobStatus(jobId);
    if (status) {
        res.json({ status });
    } else {
        res.status(404).send("Job not found");
    }
};

// export const downloadVideo = async (req: Request, res: Response) => {
//   if (!req.session?.passport?.user) {
//     res.status(401).send("Unauthorized");
//     return;
//   }
//   const id = req.params.id;
//   const finalFilePath = `public/videos/${id}`;
//   const user = await userService.getUserDetailDB(username);
//   if (!user) {
//     res.status(404).send("User not found");
//     return;
//   }
//   const userId = req.session.passport.user.data[0].id;
//   const displayName = user.display_name;
//   await youtubedl.exec(`https://www.twitch.tv/${id}`, {
//     output: finalFilePath,
//   });
//   try {
//     await downloadService.saveVideoInfo(userId, username, displayName, finalFilePath);
//     res.json({ status: "Video downloaded and info saved successfully" });
//   } catch (error) {
//     console.error("Error saving video info:", error);
//     res.status(500).send("Error saving video info");
//   }
// };
