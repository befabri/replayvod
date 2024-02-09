import { Channel, FetchLog, StreamTitle, Video } from "@prisma/client";

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
    tags: string[];
    videos: Video[];
    categories: string[];
    titles: StreamTitle[];
}
