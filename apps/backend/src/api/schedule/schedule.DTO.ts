import { Quality, Category, Tag } from ".prisma/client";
import { logger as rootLogger } from "../../app";
import { channelFeature } from "../channel";
import { videoFeature } from "../video";
import { categoryFeature } from "../category";
import { Resolution } from "../../models/dowloadModel";

const logger = rootLogger.child({ domain: "download", service: "transformUtils" });

export interface CreateScheduleDTO {
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
    requestedBy: string;
}

export interface ToggleScheduleStatusDTO {
    enable: boolean;
}

type DownloadScheduleWithoutID = {
    broadcasterId: string;
    quality: Quality;
    viewersCount?: number | null;
    isDeleteRediff: boolean;
    timeBeforeDelete?: number;
    requestedBy: string;
    hasTags: boolean;
    hasMinView: boolean;
    hasCategory: boolean;
};

type EditScheduleDTO = {
    quality: Quality;
    viewersCount?: number | null;
    isDeleteRediff: boolean;
    timeBeforeDelete?: number;
    hasTags: boolean;
    hasMinView: boolean;
    hasCategory: boolean;
};

const transformCommonSchedule = async (schedule: CreateScheduleDTO) => {
    const channel = await channelFeature.getChannelByName(schedule.channelName);
    if (!channel) {
        throw new Error("ChannelName doesn't exist");
    }
    let categories: Category[] = [];
    if (schedule.hasCategory && schedule.categories.length > 0) {
        const categoriesFetch = schedule.categories.map((categoryName) =>
            categoryFeature.getCategoryByName(categoryName)
        );
        const fetchedCategories = await Promise.all(categoriesFetch);
        categories = fetchedCategories.filter((cat): cat is Category => cat !== undefined);

        if (categories.length !== schedule.categories.length) {
            throw new Error("One or more categories do not exist");
        }
    }
    const tags = schedule.hasTags ? schedule.tags.map((name) => ({ name })) : [];
    return { channel, categories, tags };
};

export const transformDownloadSchedule = async (
    schedule: CreateScheduleDTO,
    userId: string
): Promise<{ downloadSchedule: DownloadScheduleWithoutID; tags: Tag[]; categories: Category[] }> => {
    try {
        const { channel, categories, tags } = await transformCommonSchedule(schedule);

        const transformedDownloadSchedule = {
            broadcasterId: channel.broadcasterId,
            requestedBy: userId,
            ...buildScheduleProperties(schedule),
        };

        return { downloadSchedule: transformedDownloadSchedule, tags, categories };
    } catch (error) {
        logger.error("Error transforming downloadSchedule: %s", error);
        throw error;
    }
};

export const transformDownloadScheduleEdit = async (
    schedule: CreateScheduleDTO
): Promise<{ downloadSchedule: EditScheduleDTO; tags: Tag[]; categories: Category[] }> => {
    try {
        const { categories, tags } = await transformCommonSchedule(schedule);

        const transformedDownloadSchedule = {
            ...buildScheduleProperties(schedule),
        };

        return { downloadSchedule: transformedDownloadSchedule, tags, categories };
    } catch (error) {
        logger.error("Error transforming downloadScheduleEdit: %s", error);
        throw error;
    }
};

const buildScheduleProperties = (schedule: CreateScheduleDTO) => ({
    quality: videoFeature.mapVideoQualityToQuality(schedule.quality),
    isDeleteRediff: schedule.isDeleteRediff,
    hasTags: schedule.hasTags,
    hasMinView: schedule.hasMinView,
    hasCategory: schedule.hasCategory,
    ...(schedule.isDeleteRediff && schedule.timeBeforeDelete != null
        ? { timeBeforeDelete: schedule.timeBeforeDelete }
        : {}),
    ...(schedule.hasMinView ? { viewersCount: schedule.viewersCount ?? undefined } : {}),
});
