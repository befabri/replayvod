import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";
const logger = rootLogger.child({ domain: "channel", service: "categoryService" });
import { Category, PrismaClient, Status } from "@prisma/client";
import { twitchFeature } from "../twitch";

const MAX_RETRIES = 3;
const RETRY_DELAY = 1000;

export const createCategory = async (category: Category) => {
    try {
        const existingCategory = await prisma.category.findUnique({
            where: { id: category.id },
        });
        if (!existingCategory) {
            return await prisma.category.create({
                data: category,
            });
        } else {
            logger.info("Category already exists: %s", category.name);
            return existingCategory;
        }
    } catch (error) {
        logger.error("Error creating category: %s", error);
        throw error;
    }
};

export const updateCategory = async (category: Category) => {
    try {
        return prisma.category.upsert({
            where: { id: category.id },
            update: {
                boxArtUrl: category.boxArtUrl,
                igdbId: category.igdbId,
                name: category.name,
            },
            create: category,
        });
    } catch (error) {
        logger.error("Error updating category: %s", error);
        throw error;
    }
};

export const addAllCategories = async (categories: Category[]) => {
    let attempts = 0;
    const sortedCategories = categories.sort((a, b) => a.id.localeCompare(b.id));

    while (attempts < MAX_RETRIES) {
        try {
            await prisma.category.createMany({
                data: sortedCategories,
                skipDuplicates: true,
            });
            return sortedCategories;
        } catch (error) {
            if (error instanceof Error && error.message.includes("deadlock")) {
                logger.error(`Deadlock encountered while adding/updating categories (Attempt ${attempts + 1})`, {
                    error,
                });

                if (attempts === MAX_RETRIES - 1) {
                    throw error;
                }

                await new Promise((resolve) => setTimeout(resolve, RETRY_DELAY));
                attempts++;
            } else {
                throw error;
            }
        }
    }
};

export const categoryExist = async (id: string) => {
    const category = await prisma.category.findUnique({
        where: { id: id },
    });
    return !!category;
};

export const getAllCategories = async () => {
    return prisma.category.findMany();
};

export const getCategoryById = async (id: string) => {
    return prisma.category.findUnique({ where: { id: id } });
};

export const getCategoryByName = async (name: string) => {
    return prisma.category.findFirst({ where: { name: name } });
};

export const getAllVideosCategories = async () => {
    return await prisma.category.findMany({
        where: {
            videoCategory: {
                some: {},
            },
        },
    });
};

export const getAllVideosCategoriesByStatus = async (status: Status) => {
    return await prisma.category.findMany({
        where: {
            videoCategory: {
                some: {
                    video: {
                        status: status,
                    },
                },
            },
        },
    });
};

export const createVideoCategory = async (videoId: number, categoryId: string) => {
    try {
        const existingEntry = await prisma.videoCategory.findUnique({
            where: { videoId_categoryId: { videoId: videoId, categoryId: categoryId } },
        });

        if (!existingEntry) {
            return await prisma.videoCategory.create({
                data: {
                    videoId: videoId,
                    categoryId: categoryId,
                },
            });
        } else {
            return existingEntry;
        }
    } catch (error) {
        logger.error("Error adding/updating videoCategory: %s", error);
        throw error;
    }
};

export const updateMissingBoxArtUrls = async () => {
    try {
        logger.info("Updating missing box art");
        const categoriesWithMissingBoxArt = await prisma.category.findMany({
            where: {
                boxArtUrl: "",
            },
        });
        for (const category of categoriesWithMissingBoxArt) {
            const fetchedGame = await twitchFeature.getGameDetail(category.id);
            await prisma.category.update({
                where: { id: category.id },
                data: { boxArtUrl: fetchedGame?.boxArtUrl, igdbId: fetchedGame?.igdbId },
            });
        }
        logger.info("Box Art URLs updated successfully");
    } catch (error) {
        logger.error("Error updating categories with box art URLs: %s", error);
        throw error;
    }
};

export const createStreamCategory = async (
    streamId: string,
    categoryId: string,
    prismaInstance: PrismaClient = prisma
) => {
    try {
        const existingEntry = await prismaInstance.streamCategory.findUnique({
            where: { streamId_categoryId: { streamId: streamId, categoryId: categoryId } },
        });
        if (!existingEntry) {
            return await prismaInstance.streamCategory.create({
                data: {
                    streamId: streamId,
                    categoryId: categoryId,
                },
            });
        } else {
            return existingEntry;
        }
    } catch (error) {
        logger.error("Error adding/updating streamCategory: %s", error);
        throw error;
    }
};

export const createDownloadScheduleCategory = async (downloadScheduleId: number, categoryId: string) => {
    try {
        const existingEntry = await prisma.downloadScheduleCategory.findUnique({
            where: {
                downloadScheduleId_categoryId: { downloadScheduleId: downloadScheduleId, categoryId: categoryId },
            },
        });
        if (!existingEntry) {
            return await prisma.downloadScheduleCategory.create({
                data: {
                    downloadScheduleId: downloadScheduleId,
                    categoryId: categoryId,
                },
            });
        } else {
            return existingEntry;
        }
    } catch (error) {
        logger.error("Error adding downloadScheduleCategory: %s", error);
        throw error;
    }
};
