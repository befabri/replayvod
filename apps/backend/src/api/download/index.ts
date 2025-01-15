import { JobService } from "../../services/service.job";
import { VideoRepository } from "../video/video.repository";
import { DownloadHandler } from "./download.handler";
import { DownloadService } from "./download.service";
import { PrismaClient } from "@prisma/client/extension";

export type DownloadModule = {
    service: DownloadService;
    handler: DownloadHandler;
};

export const downloadModule = (
    db: PrismaClient,
    videoRepository: VideoRepository,
    jobService: JobService
): DownloadModule => {
    const service = new DownloadService(db, videoRepository, jobService);
    const handler = new DownloadHandler();

    return {
        service,
        handler,
    };
};
