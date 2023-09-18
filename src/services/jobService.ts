import { downloadService } from "../services";
import { v4 as uuidv4 } from "uuid";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
import { Job, Status } from "@prisma/client";
const logger = rootLogger.child({ service: "jobService" });

const jobs: Map<string, Job> = new Map<string, Job>();

export const createJobId = (): string => {
    return uuidv4();
};

export const createJob = async (id: string, func: () => Promise<void>) => {
    if (await isJobExists(id)) {
        logger.error("Job already exist");
        return;
    }
    const job: Job = { id, status: Status.PENDING };
    jobs.set(id, job);

    await createNewJob(job.id, job.status);

    Promise.resolve()
        .then(async () => {
            job.status = Status.RUNNING;
            await updateJobStatus(id, Status.RUNNING);
            return func();
        })
        .then(async () => {
            job.status = Status.DONE;
            await updateJobStatus(id, Status.DONE);
        })
        .catch(async (error) => {
            logger.error("Job failed:", error);
            job.status = Status.FAILED;
            await updateJobStatus(id, Status.FAILED);
            await downloadService.setVideoFailed(id);
        });
};

export const createNewJob = async (id: string, status) => {
    try {
        const job = await prisma.job.create({
            data: {
                id: id,
                status: status,
            },
        });
        return job;
    } catch (error) {
        logger.error("Failed to create job:", error);
        throw error;
    }
};

const updateJobStatus = async (jobId: string, newStatus: Status) => {
    try {
        await prisma.job.update({
            where: { id: jobId },
            data: { status: newStatus },
        });
        logger.info(`Job ${jobId} status updated to ${newStatus}`);
    } catch (error) {
        logger.error(`Failed to update job ${jobId} status to ${newStatus}:`, error);
    }
};

export const getJobStatus = (id: string): string | undefined => {
    return jobs.get(id)?.status;
};

export const isJobExists = async (id: string): Promise<boolean> => {
    const job = await prisma.job.findUnique({
        where: {
            id: id,
        },
    });
    return !!job;
};

export const findPendingJobByBroadcasterId = async (broadcasterId: string) => {
    return prisma.video.findFirst({
        where: {
            broadcasterId: broadcasterId,
            status: Status.PENDING,
        },
    });
};

export default {
    createJobId,
    createJob,
    getJobStatus,
    findPendingJobByBroadcasterId,
};
