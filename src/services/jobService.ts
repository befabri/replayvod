import { getDbInstance } from "../models/db";
import downloadService from "./downloadService";
import { v4 as uuidv4 } from "uuid";

interface Job {
  id: string;
  status: "Pending" | "Running" | "Done" | "Failed";
}

class JobService {
  private jobs: Map<string, Job> = new Map<string, Job>();

  createJobId(): string {
    return uuidv4();
  }

  async createJob(id: string, func: () => Promise<void>) {
    const job: Job = { id, status: "Pending" };
    this.jobs.set(id, job);

    const db = await getDbInstance();
    const jobCollection = db.collection("jobs");
    await jobCollection.insertOne(job);

    Promise.resolve()
      .then(async () => {
        job.status = "Running";
        await jobCollection.updateOne({ id }, { $set: { status: "Running" } });
        return func();
      })
      .then(async () => {
        job.status = "Done";
        await jobCollection.updateOne({ id }, { $set: { status: "Done" } });
      })
      .catch(async (error) => {
        console.error("Job failed:", error);
        job.status = "Failed";
        await jobCollection.updateOne({ id }, { $set: { status: "Failed" } });
        await downloadService.setVideoFailed(id);
      });
  }

  getJobStatus(id: string): string | undefined {
    return this.jobs.get(id)?.status;
  }
}

export default JobService;
