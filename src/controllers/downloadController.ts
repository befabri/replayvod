import { Request, Response } from "express";
import DownloadService from "../services/downloadService";
import UserService from "../services/userService";
import JobService from "../services/jobService";
import { User } from "../models/userModel";

const jobService = new JobService();
const userService = new UserService();
const downloadService = new DownloadService();

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

export const downloadStream = async (req: Request, res: Response) => {
  if (!req.session?.passport?.user) {
    res.status(401).send("Unauthorized");
    return;
  }
  const broadcaster_id = req.params.id;
  const user = (await userService.getUserDetailDB(broadcaster_id)) as User;
  if (!user) {
    res.status(404).send("User not found");
    return;
  }
  const userId = req.session.passport.user.data[0].id;
  const displayName = user.display_name;
  const finalFilePath = `public/videos/${displayName}.mp4`;
  const cookiesFilePath = `cookies.txt`;
  const jobId = jobService.createJob(async () => {
    try {
      const videoName = await downloadService.startDownload(
        userId,
        broadcaster_id,
        displayName,
        finalFilePath,
        cookiesFilePath
      );
      await downloadService.finishDownload(videoName);
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
