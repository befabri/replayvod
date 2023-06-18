import { v4 as uuidv4 } from "uuid";

interface Job {
  id: string;
  status: "Pending" | "Running" | "Done" | "Failed";
}

class JobService {
  private jobs: Map<string, Job> = new Map<string, Job>();

  createJob(func: () => Promise<void>): string {
    const id = uuidv4();
    const job: Job = { id, status: "Pending" };
    this.jobs.set(id, job);

    Promise.resolve()
      .then(() => {
        job.status = "Running";
        return func();
      })
      .then(() => (job.status = "Done"))
      .catch((error) => {
        console.error("Job failed:", error);
        job.status = "Failed";
      });

    return id;
  }

  getJobStatus(id: string): string | undefined {
    return this.jobs.get(id)?.status;
  }
}

export default JobService;
