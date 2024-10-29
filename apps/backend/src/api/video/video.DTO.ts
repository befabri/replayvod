import { Category, Channel, Quality, Status } from "@prisma/client";

export interface videoDTO {
    id: number;
    filename: string;
    status: Status;
    displayName: string;
    startDownloadAt: string;
    downloadedAt: string;
    viewerCount: number;
    language: string;
    quality: Quality;
    duration: number;
    size: number;
    thumbnail: string;
    broadcasterId: string;
    jobId: string;
    streamId: string;
    channel: Channel;
    tags: string[];
    titles: string[];
    videoCategory: Category[];
}

export const transformVideo = (videos: any[]): videoDTO[] => {
    return videos.map((video) => {
        return {
            ...video,
            tags: video.tags.map((t: { tag: { name: string } }) => t.tag.name),
            titles: video.titles.map((t: { title: { name: string } }) => t.title.name),
            videoCategory: video.videoCategory.map((v: { category: Category }) => v.category),
        };
    });
};
