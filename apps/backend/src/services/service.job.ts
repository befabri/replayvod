import { v4 as uuidv4 } from "uuid";
import { logger as rootLogger } from "../app";
import { Job, PrismaClient, Status } from "@prisma/client";
const logger = rootLogger.child({ domain: "service", service: "job" });

type JobFunc = (jobId: string) => Promise<void>;
type FailureHandler = (jobId: string) => Promise<void>;

export class JobService {
    constructor(private db: PrismaClient) {}

    private static jobs: Map<string, Job> = new Map<string, Job>();

    private static createJobId(): string {
        return uuidv4();
    }

    async createJob(func: JobFunc, failureHandler?: FailureHandler): Promise<string> {
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
            if (failureHandler) {
                await failureHandler(id);
            }
        }
        return id;
    }

    private async createNewJob(id: string, status: Status): Promise<Job> {
        try {
            return await this.db.job.create({
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
            await this.db.job.update({
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
        const job = await this.db.job.findUnique({
            where: {
                id: id,
            },
        });
        return !!job;
    }
}
