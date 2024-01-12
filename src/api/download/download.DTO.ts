import { Quality, Category, Tag } from ".prisma/client";
import { tagService } from "../../services";
import { logger as rootLogger } from "../../app";
import { channelFeature } from "../channel";
import { videoFeature } from "../video";
import { categoryFeature } from "../category";

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

export interface ScheduleToggleDTO {
    enable: boolean;
}

type DownloadScheduleWithoutID = {
    broadcasterId: string;
    quality: Quality;
    viewersCount?: number | null;
    isDeleteRediff: boolean;
    timeBeforeDelete?: number;
    requestedBy: string;
};

type DownloadScheduleEdit = {
    quality: Quality;
    viewersCount?: number | null;
    isDeleteRediff: boolean;
    timeBeforeDelete?: number;
};

const splitByTag = (str: string): string[] => {
    return str.split(",");
};

const getTags = async (tagsStr: string): Promise<Tag[]> => {
    const tags = splitByTag(tagsStr);
    return await tagService.createMultipleTags(tags.map((tag) => ({ name: tag })));
};

// TODO verify
export const transformDownloadSchedule = async (
    schedule: DownloadScheduleDTO,
    userId: string
): Promise<{ downloadSchedule: DownloadScheduleWithoutID; tags: Tag[]; category: Category | null }> => {
    try {
        const channel = await channelFeature.getChannelByName(schedule.channelName);
        if (!channel) {
            throw new Error("ChannelName doesn't exist");
        }
        const transformedDownloadSchedule = {
            broadcasterId: channel.broadcasterId,
            quality: videoFeature.mapVideoQualityToQuality(schedule.quality),
            isDeleteRediff: schedule.isDeleteRediff,
            requestedBy: userId,
            ...(schedule.isDeleteRediff && schedule.timeBeforeDelete != null
                ? { timeBeforeDelete: schedule.timeBeforeDelete }
                : {}),
            ...(schedule.hasMinView ? { viewersCount: schedule.viewersCount ?? undefined } : {}),
        };
        const tags = schedule.hasTags && schedule.tag ? await getTags(schedule.tag) : [];
        let category = null;
        if (schedule.hasCategory) {
            if (schedule.category) {
                category = await categoryFeature.getCategoryByName(schedule.category);
                if (!category) {
                    throw new Error("Category doesn't exist");
                }
            } else {
                throw new Error("Category information is missing or incomplete");
            }
        }
        return {
            downloadSchedule: transformedDownloadSchedule,
            tags,
            category,
        };
    } catch (error) {
        if (error instanceof Error) {
            logger.error(`Error transforming downloadSchedule: ${error.message}`);
        } else {
            logger.error(`Error transforming downloadSchedule`);
        }
        throw error;
    }
};

export const transformDownloadScheduleEdit = async (
    schedule: DownloadScheduleDTO
): Promise<{ downloadSchedule: DownloadScheduleEdit; tags: Tag[]; category: Category | null }> => {
    try {
        const channel = await channelFeature.getChannelByName(schedule.channelName);
        if (!channel) {
            throw new Error("ChannelName doesn't exist");
        }
        const transformedDownloadSchedule = {
            quality: videoFeature.mapVideoQualityToQuality(schedule.quality),
            isDeleteRediff: schedule.isDeleteRediff,
            ...(schedule.isDeleteRediff && schedule.timeBeforeDelete != null
                ? { timeBeforeDelete: schedule.timeBeforeDelete }
                : {}),
            ...(schedule.hasMinView ? { viewersCount: schedule.viewersCount ?? undefined } : {}),
        };
        const tags = schedule.hasTags && schedule.tag ? await getTags(schedule.tag) : [];
        let category = null;
        if (schedule.hasCategory) {
            if (schedule.category) {
                category = await categoryFeature.getCategoryByName(schedule.category);
                if (!category) {
                    throw new Error("Category doesn't exist");
                }
            } else {
                throw new Error("Category information is missing or incomplete");
            }
        }
        return {
            downloadSchedule: transformedDownloadSchedule,
            tags,
            category,
        };
    } catch (error) {
        if (error instanceof Error) {
            logger.error(`Error transforming downloadSchedule: ${error.message}`);
        } else {
            logger.error(`Error transforming downloadSchedule`);
        }
        throw error;
    }
};
