export interface Video {
    id: number;
    filename: string;
    status: "PENDING" | "COMPLETED" | "FAILED";
    displayName: string;
    startDownloadAt: Date;
    downloadedAt: Date | null;
    viewerCount: number;
    language: string;
    quality: "LOW" | "MEDIUM" | "HIGH";
    duration: number | null;
    size: number | null;
    thumbnail: string | null;
    broadcasterId: string;
    jobId: string;
    streamId: string;
    tags: {
        tag: {
            name: string;
        };
    }[];
    titles: {
        title: {
            name: string;
        };
    }[];
    videoCategory: {
        videoId: number;
        categoryId: string;
        category: {
            name: string;
        };
    }[];

    isChecked?: boolean;
}

export interface TableProps {
    items: Video[];
}

export interface Task {
    _id?: string;
    id: string;
    name: string;
    description: string;
    taskType: string;
    metadata?: {
        [key: string]: string;
    };
    interval: number;
    lastExecution: Date;
    lastDuration: number;
    nextExecution: Date;
}

export interface Log {
    _id?: string;
    id: number;
    filename: string;
    downloadUrl: string;
    lastWriteTime: string;
    type: "YoutubeDl" | "FixVideos" | "Request" | "Error" | "Combined" | "Info" | "Connection";
}

export interface EventSub {
    _id?: string;
    id: string;
    status: string;
    type: string;
    user_id: string;
    user_login?: string;
    created_at: Date;
    cost: number;
}

export interface EventSubCost {
    total: number;
    total_cost: number;
    max_total_cost: number;
}

export interface Stream {
    id: string;
    isMature: true;
    language: string;
    startedAt: string;
    thumbnailUrl: string;
    type: string;
    broadcasterId: string;
    viewerCount: number;
    fetchId: string;
}

export interface Channel {
    broadcasterId: string;
    broadcasterLogin: string;
    broadcasterName: string;
    displayName: string;
    broadcasterType: string;
    createdAt: string;
    description: string;
    offlineImageUrl: string;
    profileImageUrl: string;
    profilePicture: string;
    type: string;
    viewCount: number;
}
