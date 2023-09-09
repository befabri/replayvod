export interface Log {
    id: number;
    filename: string;
    downloadUrl: string;
    lastWriteTime: Date;
    type: "YoutubeDl" | "FixVideos" | "Request" | "Error" | "Combined" | "Info" | "Connection";
}
