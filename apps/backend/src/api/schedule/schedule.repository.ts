import { logger as rootLogger } from "../../app";
import { PrismaClientKnownRequestError } from "@prisma/client/runtime/library";
import { StreamDTO } from "../channel/channel.dto";
import { VideoRepository } from "../video/video.repository";
import { CreateScheduleDTO, ScheduleDTO } from "./schedule.dto";
import { PrismaClient } from "@prisma/client";
const logger = rootLogger.child({ domain: "schedule", service: "repository" });

export class ScheduleRepository {
    constructor(
        private db: PrismaClient,
        private videoRepository: VideoRepository,
        private dto: ScheduleDTO
    ) {}

    getCurrentSchedulesByUser = async (userId: string) => {
        const schedules = await this.db.downloadSchedule.findMany({
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
            quality: this.videoRepository.mapQualityToVideoQuality(schedule.quality),
            channelName: schedule.channel.broadcasterLogin,
            categories: downloadScheduleCategory.map((c) => c.category.name),
            tags: downloadScheduleTag.map((t) => t.tag.name),
        }));
    };

    private matchesCriteria = (schedule: CreateScheduleDTO, streamFetched: StreamDTO) => {
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

    private getScheduleEnabledByBroadcaster = async (broadcasterId: string): Promise<CreateScheduleDTO[]> => {
        const schedules = await this.db.downloadSchedule.findMany({
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
            quality: this.videoRepository.mapQualityToVideoQuality(schedule.quality),
            channelName: schedule.channel.broadcasterLogin,
            categories: downloadScheduleCategory.map((c) => c.category.name),
            tags: downloadScheduleTag.map((t) => t.tag.name),
        }));
    };

    getSchedule = async (scheduleId: number, userId: string) => {
        return this.db.downloadSchedule.findUnique({
            where: {
                id: scheduleId,
                requestedBy: userId,
            },
        });
    };

    removeSchedule = async (scheduleId: number) => {
        try {
            return await this.db.downloadSchedule.delete({
                where: {
                    id: scheduleId,
                },
            });
        } catch (error) {
            logger.error("Failed to removed scheduleId %s : %s", scheduleId, error);
            return;
        }
    };

    toggleSchedule = async (scheduleId: number, enable: boolean) => {
        return await this.db.downloadSchedule.update({
            where: {
                id: scheduleId,
            },
            data: {
                isDisabled: enable,
            },
        });
    };

    async getScheduleMatch(stream: StreamDTO, broadcasterId: string): Promise<CreateScheduleDTO[]> {
        const schedules = await this.getScheduleEnabledByBroadcaster(broadcasterId);
        return schedules.filter((schedule) => this.matchesCriteria(schedule, stream));
    }

    createSchedule = async (newSchedule: CreateScheduleDTO, userId: string) => {
        try {
            const transformedScheduleData = await this.dto.transformDownloadSchedule(newSchedule, userId);
            await this.db.$transaction(async (tx) => {
                const createdDownloadSchedule = await tx.downloadSchedule.create({
                    data: transformedScheduleData.downloadSchedule,
                });

                if (transformedScheduleData.tags.length > 0) {
                    const existingTags = await tx.downloadScheduleTag.findMany({
                        where: { downloadScheduleId: createdDownloadSchedule.id },
                        select: { tagId: true },
                    });
                    const existingTagIds = existingTags.map((tag) => tag.tagId);
                    const newTagNames = transformedScheduleData.tags.map((tag) => tag.name);
                    const tagsToAdd = newTagNames.filter((tagName) => !existingTagIds.includes(tagName));
                    for (const tagName of tagsToAdd) {
                        let existingTag = await tx.tag.findUnique({
                            where: { name: tagName },
                        });
                        if (!existingTag) {
                            existingTag = await tx.tag.create({
                                data: { name: tagName },
                            });
                        }
                        await tx.downloadScheduleTag.create({
                            data: { downloadScheduleId: createdDownloadSchedule.id, tagId: existingTag.name },
                        });
                    }
                }

                if (transformedScheduleData.categories.length > 0) {
                    const existingCategories = await tx.downloadScheduleCategory.findMany({
                        where: { downloadScheduleId: createdDownloadSchedule.id },
                        select: { categoryId: true },
                    });
                    const existingCategoriesIds = existingCategories.map((category) => category.categoryId);
                    const newCategoriesIds = transformedScheduleData.categories.map((category) => category.id);
                    const categoriesToAdd = newCategoriesIds.filter(
                        (categoryId) => !existingCategoriesIds.includes(categoryId)
                    );
                    for (const categoryId of categoriesToAdd) {
                        await tx.downloadScheduleCategory.create({
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

    editSchedule = async (scheduleId: number, schedule: CreateScheduleDTO) => {
        try {
            const transformedScheduleData = await this.dto.transformDownloadScheduleEdit(schedule);
            await this.db.$transaction(async (tx) => {
                await tx.downloadSchedule.update({
                    where: { id: scheduleId },
                    data: transformedScheduleData.downloadSchedule,
                });
                if (!schedule.hasTags) {
                    await tx.downloadScheduleTag.deleteMany({
                        where: { downloadScheduleId: scheduleId },
                    });
                }
                if (transformedScheduleData.tags.length > 0) {
                    const existingTags = await tx.downloadScheduleTag.findMany({
                        where: { downloadScheduleId: scheduleId },
                        select: { tagId: true },
                    });
                    const existingTagIds = existingTags.map((tag) => tag.tagId);
                    const newTagNames = transformedScheduleData.tags.map((tag) => tag.name);
                    const tagsToAdd = newTagNames.filter((tagName) => !existingTagIds.includes(tagName));
                    const tagsToRemove = existingTagIds.filter((tagId) => !newTagNames.includes(tagId));
                    for (const tagName of tagsToAdd) {
                        let existingTag = await tx.tag.findUnique({
                            where: { name: tagName },
                        });
                        if (!existingTag) {
                            existingTag = await tx.tag.create({
                                data: { name: tagName },
                            });
                        }
                        await tx.downloadScheduleTag.create({
                            data: { downloadScheduleId: scheduleId, tagId: existingTag.name },
                        });
                    }
                    for (const tagId of tagsToRemove) {
                        await tx.downloadScheduleTag.deleteMany({
                            where: { downloadScheduleId: scheduleId, tagId },
                        });
                    }
                }
                if (!schedule.hasCategory) {
                    await tx.downloadScheduleCategory.deleteMany({
                        where: { downloadScheduleId: scheduleId },
                    });
                }
                if (transformedScheduleData.categories.length > 0) {
                    const existingCategories = await tx.downloadScheduleCategory.findMany({
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
                        await tx.downloadScheduleCategory.create({
                            data: { downloadScheduleId: scheduleId, categoryId: categoryId },
                        });
                    }
                    for (const categoryId of categoriesToRemove) {
                        await tx.downloadScheduleCategory.deleteMany({
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
}
