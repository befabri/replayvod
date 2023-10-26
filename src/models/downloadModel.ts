export enum VideoQuality {
    LOW = "480",
    MEDIUM = "720",
    HIGH = "1080",
}

export type Resolution = "1080" | "720" | "480" | "360" | "160";

export type FallbackResolutions = {
    [key in Resolution]: Resolution[];
};
