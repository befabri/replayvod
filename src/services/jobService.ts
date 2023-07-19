import { getDbInstance } from "../models/db";
import { downloadService } from "../services";
import { v4 as uuidv4 } from "uuid";

interface Job {
    id: string;
    status: "Pending" | "Running" | "Done" | "Failed";
}

const jobs: Map<string, Job> = new Map<string, Job>();

export const createJobId = (): string => {
    return uuidv4();
};

export const createJob = async (id: string, func: () => Promise<void>) => {
    const job: Job = { id, status: "Pending" };
    jobs.set(id, job);

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
};

export const getJobStatus = (id: string): string | undefined => {
    return jobs.get(id)?.status;
};

export const findPendingJob = async (broadcaster_id: string) => {
    const db = await getDbInstance();
    const jobCollection = db.collection("videos");
    return jobCollection.findOne({ broadcaster_id, status: "Pending" });
};

export default {
    createJobId,
    createJob,
    getJobStatus,
    findPendingJob,
};
