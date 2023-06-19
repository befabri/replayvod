import { Request, Response } from "express";
import DownloadService from "../services/downloadService";
import UserService from "../services/userService";
import JobService from "../services/jobService";
import ScheduleService from "../services/scheduleService";
import { User } from "../models/twitchModel";
import TwitchAPI from "../utils/twitchAPI";
import { DownloadSchedule } from "../models/downloadModel";
import moment from "moment-timezone";

const jobService = new JobService();
const userService = new UserService();
const downloadService = new DownloadService();
const twitchAPI = new TwitchAPI();
const scheduleService = new ScheduleService();

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
  console.log(data)
  if (!data.source || !data.channelName || !data.trigger || !data.quality) {
    res.status(400).send("Invalid request data");
    return;
  }

  data.requested_by = req.session.passport.user.data[0].id;
  try {
    await scheduleService.insertIntoDb(data);
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
  const userId = req.session.passport.user.data[0].id;
  const currentDate = moment().format("DDMMYYYY-HHmmss");
  const filename = `${user.display_name.toLowerCase()}_${currentDate}.mp4`;
  const finalFilePath = `public/videos/${filename}`;
  const cookiesFilePath = `cookies.txt`;
  const pendingJob = await downloadService.findPendingJob(broadcasterId);
  if (pendingJob) {
    res
      .status(400)
      .json({ message: "There is already a job running for this broadcaster.", jobId: pendingJob.id });
    return;
  }

  const jobId = jobService.createJobId();
  jobService.createJob(jobId, async () => {
    try {
      const video = await downloadService.startDownload(
        userId,
        broadcasterId,
        user.display_name,
        user.login,
        finalFilePath,
        cookiesFilePath,
        jobId,
        stream
      );
      await downloadService.finishDownload(finalFilePath);
    } catch (error) {
      console.error("Error saving video info:", error);
      throw error;
    }
  });

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
