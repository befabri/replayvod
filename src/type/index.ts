export interface Video {
    _id?: string;
    id: string;
    filename: string;
    status: string;
    display_name: string;
    broadcaster_id: string;
    requested_by: string;
    start_download_at: Date;
    downloaded_at: Date;
    job_id: string;
    category: { id: string; name: string }[];
    title: string[];
    tags: string[];
    viewer_count: number[];
    language: string;
    size?: number;
    thumbnail?: string;
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
    broadcaster_user_id: string;
    broadcaster_login?: string;
    created_at: Date;
    cost: number;
}

export interface EventSubCost {
    total: number;
    total_cost: number;
    max_total_cost: number;
}
