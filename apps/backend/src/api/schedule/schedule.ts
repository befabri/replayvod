import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { videoFeature } from "../video";
import { CreateScheduleDTO, transformDownloadSchedule, transformDownloadScheduleEdit } from "./schedule.DTO";
import { PrismaClientKnownRequestError } from "@prisma/client/runtime/library";
import { StreamDTO } from "../channel/channel.DTO";
const logger = rootLogger.child({ domain: "download", service: "downloadService" });

export const getCurrentSchedulesByUser = async (userId: string) => {
    const schedules = await prisma.downloadSchedule.findMany({
        where: {
            requestedBy: userId,
        },
        include: {
            channel: true,
            downloadScheduleTag: {
                select: {
                    tag: {
                        select: {
                            name: true,
                        },
                    },
                },
            },
            downloadScheduleCategory: {
                select: {
                    category: true,
                },
            },
        },
    });
    return schedules.map(({ downloadScheduleCategory, downloadScheduleTag, ...schedule }) => ({
        ...schedule,
        quality: videoFeature.mapQualityToVideoQuality(schedule.quality),
        channelName: schedule.channel.broadcasterLogin,
        categories: downloadScheduleCategory.map((c) => c.category.name),
        tags: downloadScheduleTag.map((t) => t.tag.name),
    }));
};

export const matchesCriteria = (schedule: CreateScheduleDTO, streamFetched: StreamDTO) => {
    logger.info("Match criteria....");
    const meetsViewerCountCriteria =
        !schedule.hasMinView ||
        (schedule.viewersCount !== null &&
            schedule.viewersCount &&
            streamFetched.viewerCount >= schedule.viewersCount);
    const meetsCategoryCriteria =
        !schedule.hasCategory ||
        streamFetched.categories.some((category) => schedule.categories.includes(category.name));
    const meetsTagsCriteria =
        !schedule.hasTags ||
        streamFetched.tags.some((tag) =>
            schedule.tags.map((scheduleTag) => scheduleTag.toLowerCase()).includes(tag.name.toLowerCase())
        );
    return meetsViewerCountCriteria && meetsCategoryCriteria && meetsTagsCriteria;
};

export const getScheduleEnabledByBroadcaster = async (broadcasterId: string): Promise<CreateScheduleDTO[]> => {
    const schedules = await prisma.downloadSchedule.findMany({
        where: {
            broadcasterId: broadcasterId,
            isDisabled: false,
        },
        include: {
            channel: true,
            downloadScheduleTag: {
                select: {
                    tag: {
                        select: {
                            name: true,
                        },
                    },
                },
            },
            downloadScheduleCategory: {
                select: {
                    category: true,
                },
            },
        },
    });
    return schedules.map(({ downloadScheduleCategory, downloadScheduleTag, ...schedule }) => ({
        ...schedule,
        quality: videoFeature.mapQualityToVideoQuality(schedule.quality),
        channelName: schedule.channel.broadcasterLogin,
        categories: downloadScheduleCategory.map((c) => c.category.name),
        tags: downloadScheduleTag.map((t) => t.tag.name),
    }));
};

export const getSchedule = async (scheduleId: number, userId: string) => {
    return prisma.downloadSchedule.findUnique({
        where: {
            id: scheduleId,
            requestedBy: userId,
        },
    });
};

export const removeSchedule = async (scheduleId: number) => {
    try {
        return await prisma.downloadSchedule.delete({
            where: {
                id: scheduleId,
            },
        });
    } catch (error) {
        logger.error("Failed to removed scheduleId %s : %s", scheduleId, error);
        return;
    }
};

export const toggleSchedule = async (scheduleId: number, enable: boolean) => {
    return await prisma.downloadSchedule.update({
        where: {
            id: scheduleId,
        },
        data: {
            isDisabled: enable,
        },
    });
};

export async function getScheduleMatch(stream: StreamDTO, broadcasterId: string): Promise<CreateScheduleDTO[]> {
    const schedules = await getScheduleEnabledByBroadcaster(broadcasterId);
    return schedules.filter((schedule) => matchesCriteria(schedule, stream));
}

export const createSchedule = async (newSchedule: CreateScheduleDTO, userId: string) => {
    try {
        const transformedScheduleData = await transformDownloadSchedule(newSchedule, userId);
        await prisma.$transaction(async (prisma) => {
            const createdDownloadSchedule = await prisma.downloadSchedule.create({
                data: transformedScheduleData.downloadSchedule,
            });

            if (transformedScheduleData.tags.length > 0) {
                const existingTags = await prisma.downloadScheduleTag.findMany({
                    where: { downloadScheduleId: createdDownloadSchedule.id },
                    select: { tagId: true },
                });
                const existingTagIds = existingTags.map((tag) => tag.tagId);
                const newTagNames = transformedScheduleData.tags.map((tag) => tag.name);
                const tagsToAdd = newTagNames.filter((tagName) => !existingTagIds.includes(tagName));
                for (const tagName of tagsToAdd) {
                    let existingTag = await prisma.tag.findUnique({
                        where: { name: tagName },
                    });
                    if (!existingTag) {
                        existingTag = await prisma.tag.create({
                            data: { name: tagName },
                        });
                    }
                    await prisma.downloadScheduleTag.create({
                        data: { downloadScheduleId: createdDownloadSchedule.id, tagId: existingTag.name },
                    });
                }
            }

            if (transformedScheduleData.categories.length > 0) {
                const existingCategories = await prisma.downloadScheduleCategory.findMany({
                    where: { downloadScheduleId: createdDownloadSchedule.id },
                    select: { categoryId: true },
                });
                const existingCategoriesIds = existingCategories.map((category) => category.categoryId);
                const newCategoriesIds = transformedScheduleData.categories.map((category) => category.id);
                const categoriesToAdd = newCategoriesIds.filter(
                    (categoryId) => !existingCategoriesIds.includes(categoryId)
                );
                for (const categoryId of categoriesToAdd) {
                    await prisma.downloadScheduleCategory.create({
                        data: { downloadScheduleId: createdDownloadSchedule.id, categoryId: categoryId },
                    });
                }
            }
        });
    } catch (error) {
        if (error instanceof PrismaClientKnownRequestError) {
            if (error.code === "P2002") {
                throw new Error("User is already assigned to this broadcaster ID");
            }
        }
        throw error;
    }
};

export const editSchedule = async (scheduleId: number, schedule: CreateScheduleDTO) => {
    try {
        const transformedScheduleData = await transformDownloadScheduleEdit(schedule);
        await prisma.$transaction(async (prisma) => {
            await prisma.downloadSchedule.update({
                where: { id: scheduleId },
                data: transformedScheduleData.downloadSchedule,
            });
            if (!schedule.hasTags) {
                await prisma.downloadScheduleTag.deleteMany({
                    where: { downloadScheduleId: scheduleId },
                });
            }
            if (transformedScheduleData.tags.length > 0) {
                const existingTags = await prisma.downloadScheduleTag.findMany({
                    where: { downloadScheduleId: scheduleId },
                    select: { tagId: true },
                });
                const existingTagIds = existingTags.map((tag) => tag.tagId);
                const newTagNames = transformedScheduleData.tags.map((tag) => tag.name);
                const tagsToAdd = newTagNames.filter((tagName) => !existingTagIds.includes(tagName));
                const tagsToRemove = existingTagIds.filter((tagId) => !newTagNames.includes(tagId));
                for (const tagName of tagsToAdd) {
                    let existingTag = await prisma.tag.findUnique({
                        where: { name: tagName },
                    });
                    if (!existingTag) {
                        existingTag = await prisma.tag.create({
                            data: { name: tagName },
                        });
                    }
                    await prisma.downloadScheduleTag.create({
                        data: { downloadScheduleId: scheduleId, tagId: existingTag.name },
                    });
                }
                for (const tagId of tagsToRemove) {
                    await prisma.downloadScheduleTag.deleteMany({
                        where: { downloadScheduleId: scheduleId, tagId },
                    });
                }
            }
            if (!schedule.hasCategory) {
                await prisma.downloadScheduleCategory.deleteMany({
                    where: { downloadScheduleId: scheduleId },
                });
            }
            if (transformedScheduleData.categories.length > 0) {
                const existingCategories = await prisma.downloadScheduleCategory.findMany({
                    where: { downloadScheduleId: scheduleId },
                    select: { categoryId: true },
                });
                const existingCategoriesIds = existingCategories.map((category) => category.categoryId);
                const newCategoriesIds = transformedScheduleData.categories.map((category) => category.id);
                const categoriesToAdd = newCategoriesIds.filter(
                    (category) => !existingCategoriesIds.includes(category)
                );
                const categoriesToRemove = existingCategoriesIds.filter(
                    (category) => !newCategoriesIds.includes(category)
                );
                for (const categoryId of categoriesToAdd) {
                    await prisma.downloadScheduleCategory.create({
                        data: { downloadScheduleId: scheduleId, categoryId: categoryId },
                    });
                }
                for (const categoryId of categoriesToRemove) {
                    await prisma.downloadScheduleCategory.deleteMany({
                        where: { downloadScheduleId: scheduleId, categoryId },
                    });
                }
            }
        });
    } catch (error) {
        if (error instanceof PrismaClientKnownRequestError) {
            if (error.code === "P2002") {
                throw new Error("User is already assigned to this broadcaster ID");
            }
        }
        throw error;
    }
};