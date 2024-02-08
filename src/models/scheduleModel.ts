import { Resolution } from "./downloadModel";

export interface ScheduleDTO {
    channelName: string;
    quality: Resolution;
    hasTags: boolean;
    tags: string[];
    hasMinView: boolean;
    viewersCount?: number | null;
    hasCategory: boolean;
    categories: string[];
    timeBeforeDelete?: number | null;
    isDeleteRediff: boolean;
    requestedBy?: string;
}
