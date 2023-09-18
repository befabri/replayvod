import { Channel, Prisma, Quality, Stream } from "@prisma/client";

export interface DownloadParams {
    requestingUserId: string;
    channel: Channel;
    jobId: string;
    stream: StreamWithRelations;
    videoQuality: Quality;
}

export interface JobDetail {
    stream: StreamWithRelations;
    userId: string;
    channel: Channel;
    jobId: string;
    quality: Quality;
}

export type StreamWithRelations = Prisma.StreamGetPayload<{
    include: {
        channel: true;
        fetchLog: true;
        tags: true;
        videos: true;
        categories: true;
        titles: true;
    };
}>;
