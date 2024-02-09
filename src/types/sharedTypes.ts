import { Category, Channel, Prisma, PrismaClient, Quality, Stream, Tag, Title } from "@prisma/client";
import { StreamDTO } from "../api/channel/channel.DTO";

export interface DownloadParams {
    requestingUserId: string;
    channel: Channel;
    jobId: string;
    stream: StreamDTO;
    videoQuality: Quality;
}

export interface JobDetail {
    stream: StreamDTO;
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

export interface CreateStreamEntry {
    fetchId: string;
    stream: Stream;
    tags: Tag[];
    category: Category;
    title: Omit<Title, "id">;
}

export type TransactionalPrismaClient = Omit<
    PrismaClient,
    "$connect" | "$disconnect" | "$on" | "$transaction" | "$use" | "$extend"
>;
