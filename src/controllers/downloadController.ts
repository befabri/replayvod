import { Request, Response } from "express";
import DownloadService from "../services/downloadService";

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
