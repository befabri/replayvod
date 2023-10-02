import { Quality, Category, Tag } from ".prisma/client";
import * as channelService from "../../api/channel";
import * as videoService from "../../api/video";
import * as categoryService from "../../api/category";
import { tagService } from "../../services";

import { logger as rootLogger } from "../../app";

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
        const tags = schedule.tag ? await getTags(schedule.tag) : [];
        const category = schedule.category ? await categoryService.getCategoryByName(schedule.category) : null;
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
