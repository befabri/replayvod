import { v4 as uuidv4 } from "uuid";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
import { Job, Status, Video } from "@prisma/client";
import { downloadFeature } from "../api/download";
const logger = rootLogger.child({ domain: "download", service: "jobService" });

type JobFunc = (jobId: string) => Promise<void>;

class JobService {
    private static jobs: Map<string, Job> = new Map<string, Job>();

    private static createJobId(): string {
        return uuidv4();
    }

    async createJob(func: JobFunc): Promise<string> {
        const id = JobService.createJobId();
        const job: Job = { id, status: Status.PENDING };
        JobService.jobs.set(id, job);
        try {
            await this.createNewJob(id, Status.PENDING);
            job.status = Status.RUNNING;
            await this.updateJobStatus(id, Status.RUNNING);
            await func(id);
            job.status = Status.DONE;
            await this.updateJobStatus(id, Status.DONE);
        } catch (error) {
            logger.error("Job failed:", error);
            job.status = Status.FAILED;
            await this.updateJobStatus(id, Status.FAILED);
            await downloadFeature.setVideoFailed(id);
        }
        return id;
    }

    private async createNewJob(id: string, status: Status): Promise<Job> {
        try {
            return await prisma.job.create({
                data: {
                    id: id,
                    status: status,
                },
            });
        } catch (error) {
            logger.error("Failed to create job:", error);
            throw error;
        }
    }

    private async updateJobStatus(jobId: string, newStatus: Status): Promise<void> {
        try {
            await prisma.job.update({
                where: { id: jobId },
                data: { status: newStatus },
            });
            logger.info(`Job ${jobId} status updated to ${newStatus}`);
        } catch (error) {
            logger.error(`Failed to update job ${jobId} status to ${newStatus}:`, error);
        }
    }

    getJobStatus(id: string): Status | undefined {
        return JobService.jobs.get(id)?.status;
    }

    async isJobExists(id: string): Promise<boolean> {
        const job = await prisma.job.findUnique({
            where: {
                id: id,
            },
        });
        return !!job;
    }

    async findPendingJobByBroadcasterId(broadcasterId: string): Promise<Video | null> {
        return prisma.video.findFirst({
            where: {
                broadcasterId: broadcasterId,
                status: Status.PENDING,
            },
        });
    }
}

export const jobService = new JobService();
