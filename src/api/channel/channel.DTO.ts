import { Category, Channel, FetchLog, StreamTitle, Tag, Video } from "@prisma/client";

export interface StreamDTO {
    id: string;
    isMature?: boolean | null;
    language: string;
    startedAt: Date;
    endedAt?: Date | null;
    thumbnailUrl: string;
    type: string;
    broadcasterId: string;
    viewerCount: number;
    fetchId: string;
    channel: Channel;
    fetchLog: FetchLog;
    tags: Tag[];
    videos: Video[];
    categories: Category[];
    titles: StreamTitle[];
}
