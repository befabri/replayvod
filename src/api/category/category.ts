import { logger as rootLogger } from "@app";
import { prisma } from "@server";
const logger = rootLogger.child({ domain: "channel", service: "categoryService" });
import { Category } from "@prisma/client";

const MAX_RETRIES = 3;
const RETRY_DELAY = 1000;

export const addCategory = async (category: Category) => {
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
        logger.error("Error adding/updating category: %s", error);
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
            if (error.message && error.message.includes("deadlock")) {
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

export const getAllCategories = async () => {
    return prisma.category.findMany();
};

export const getCategoryById = async (id: string) => {
    return prisma.category.findUnique({ where: { id: id } });
};

export const getCategoryByName = async (name: string) => {
    return prisma.category.findFirst({ where: { name: name } });
};

export const addVideoCategory = async (videoId: number, categoryId: string) => {
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

export const addStreamCategory = async (streamId: string, categoryId: string) => {
    try {
        const existingEntry = await prisma.streamCategory.findUnique({
            where: { streamId_categoryId: { streamId: streamId, categoryId: categoryId } },
        });

        if (!existingEntry) {
            return await prisma.streamCategory.create({
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
