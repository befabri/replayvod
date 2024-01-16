import { tagService } from "../../services";
import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
import { CreateScheduleDTO, transformDownloadSchedule, transformDownloadScheduleEdit } from "./schedule.DTO";
import { categoryFeature } from "../category";
import { PrismaClientKnownRequestError } from "@prisma/client/runtime/library";
import { DownloadSchedule } from "@prisma/client";
const logger = rootLogger.child({ domain: "download", service: "downloadService" });

export const getCurrentSchedulesByUser = async (userId: string) => {
    return prisma.downloadSchedule.findMany({
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

export const createSchedule = async (newSchedule: CreateScheduleDTO, userId: string) => {
    try {
        const transformedScheduleData = await transformDownloadSchedule(newSchedule, userId);
        const createdDownloadSchedule = await prisma.downloadSchedule.create({
            data: transformedScheduleData.downloadSchedule,
        });

        if (transformedScheduleData.tags.length > 0) {
            await tagService.createAllDownloadScheduleTags(
                transformedScheduleData.tags.map((tag) => ({ tagId: tag.name })),
                createdDownloadSchedule.id
            );
        }

        if (transformedScheduleData.category) {
            await categoryFeature.createDownloadScheduleCategory(
                createdDownloadSchedule.id,
                transformedScheduleData.category.id
            );
        }
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

        const createdDownloadSchedule = await prisma.downloadSchedule.update({
            where: {
                id: scheduleId,
            },
            data: transformedScheduleData.downloadSchedule,
        });

        if (transformedScheduleData.tags.length > 0) {
            await tagService.createAllDownloadScheduleTags(
                transformedScheduleData.tags.map((tag) => ({ tagId: tag.name })),
                createdDownloadSchedule.id
            );
        }

        if (transformedScheduleData.category) {
            await categoryFeature.createDownloadScheduleCategory(
                createdDownloadSchedule.id,
                transformedScheduleData.category.id
            );
        }
    } catch (error) {
        if (error instanceof PrismaClientKnownRequestError) {
            if (error.code === "P2002") {
                throw new Error("User is already assigned to this broadcaster ID");
            }
        }
        throw error;
    }
};

const getScheduleByFollowedChannel = async (broadcaster_id: string) => {
    // return prisma.downloadSchedule.findFirst({
    //     where: {
    //         provider: Provider.FOLLOWED_CHANNEL,
    //         channel: {
    //             usersFollowing: {
    //                 some: {
    //                     broadcasterId: broadcaster_id,
    //                 },
    //             },
    //         },
    //     },
    // });
};

const getAllScheduleByChannel = async (broadcasterId: string): Promise<DownloadSchedule[]> => {
    return await prisma.downloadSchedule.findMany({
        where: {
            broadcasterId: broadcasterId,
        },
    });
};
