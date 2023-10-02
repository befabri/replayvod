import { Quality, Category, Tag } from ".prisma/client";
import { tagService } from "../../services";
import { logger as rootLogger } from "../../app";
import { categoryService } from "../category";
import { channelService } from "../channel";
import { videoService } from "../video";

const logger = rootLogger.child({ domain: "download", service: "transformUtils" });

export interface DownloadScheduleDTO {
    channelName: string;
    quality: Quality;
    hasTags: boolean;
    tag?: string;
    hasMinView: boolean;
    viewersCount?: number | null;
    hasCategory: boolean;
    category: string;
    timeBeforeDelete?: number | null;
    isDeleteRediff: boolean;
    requestedBy?: string;
}

type DownloadScheduleWithoutID = {
    broadcasterId: string;
    quality: Quality;
    viewersCount?: number;
    isDeleteRediff: boolean;
    timeBeforeDelete?: number;
    requestedBy: string;
};

const splitByTag = (str: string): string[] => {
    return str.split(",");
};

const getTags = async (tagsStr: string): Promise<Tag[]> => {
    const tags = splitByTag(tagsStr);
    return await tagService.addAllTags(tags.map((tag) => ({ name: tag })));
};

export const transformDownloadSchedule = async (
    schedule: DownloadScheduleDTO,
    userId: string
): Promise<{ downloadSchedule: DownloadScheduleWithoutID; tags: Tag[]; category: Category }> => {
    try {
        const broadcasterId = await channelService.getChannelBroadcasterIdByName(schedule.channelName);
        if (!broadcasterId) {
            throw new Error("ChannelName doesn't exist");
        }
        const transformedDownloadSchedule = {
            broadcasterId,
            quality: videoService.mapVideoQualityToQuality(schedule.quality),
            isDeleteRediff: schedule.isDeleteRediff,
            requestedBy: userId,
            ...(schedule.isDeleteRediff ? { timeBeforeDelete: schedule.timeBeforeDelete } : {}),
            ...(schedule.hasMinView ? { viewersCount: schedule.viewersCount } : {}),
        };
        const tags = schedule.hasTags && schedule.tag ? await getTags(schedule.tag) : [];
        const category =
            schedule.hasCategory && schedule.category
                ? await categoryService.getCategoryByName(schedule.category)
                : null;
        return {
            downloadSchedule: transformedDownloadSchedule,
            tags,
            category,
        };
    } catch (error) {
        logger.error(`Error transforming downloadSchedule: ${error.message}`);
        throw error;
    }
};
