import { logger as rootLogger } from "../../app";
import { Category, PrismaClient, Status } from "@prisma/client";
import { FETCH_MAX_RETRIES, FETCH_RETRY_DELAY } from "../../models/model.twitch";
import { TwitchService } from "../../services/service.twitch";
const logger = rootLogger.child({ domain: "category", service: "repository" });

export class CategoryRepository {
    constructor(
        private db: PrismaClient,
        private twitchService: TwitchService
    ) {}

    createCategory = async (category: Category) => {
        try {
            const existingCategory = await this.db.category.findUnique({
                where: { id: category.id },
            });
            if (!existingCategory) {
                return await this.db.category.create({
                    data: category,
                });
            } else {
                logger.debug("Category already exists: %s", category.name);
                return existingCategory;
            }
        } catch (error) {
            logger.error("Error creating category: %s", error);
            throw error;
        }
    };

    updateCategory = async (category: Category) => {
        try {
            return this.db.category.upsert({
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

    addAllCategories = async (categories: Category[]) => {
        let attempts = 0;
        const sortedCategories = categories.sort((a, b) => a.id.localeCompare(b.id));

        while (attempts < FETCH_MAX_RETRIES) {
            try {
                await this.db.category.createMany({
                    data: sortedCategories,
                    skipDuplicates: true,
                });
                return sortedCategories;
            } catch (error) {
                if (error instanceof Error && error.message.includes("deadlock")) {
                    logger.error(
                        `Deadlock encountered while adding/updating categories (Attempt ${attempts + 1})`,
                        {
                            error,
                        }
                    );

                    if (attempts === FETCH_MAX_RETRIES - 1) {
                        throw error;
                    }

                    await new Promise((resolve) => setTimeout(resolve, FETCH_RETRY_DELAY));
                    attempts++;
                } else {
                    throw error;
                }
            }
        }
    };

    categoryExist = async (id: string) => {
        const category = await this.db.category.findUnique({
            where: { id: id },
        });
        return !!category;
    };

    getAllCategories = async () => {
        return this.db.category.findMany();
    };

    getCategoryById = async (id: string) => {
        return this.db.category.findUnique({ where: { id: id } });
    };

    getCategoryByName = async (name: string) => {
        const categories = await this.db.category.findMany();
        return categories.find((category) => category.name.toLowerCase() === name.toLowerCase());
    };

    getAllVideosCategories = async (userId: string) => {
        return await this.db.category.findMany({
            where: {
                videoCategory: {
                    some: {
                        video: {
                            VideoRequest: {
                                some: {
                                    userId: userId,
                                },
                            },
                        },
                    },
                },
            },
        });
    };

    getAllVideosCategoriesByStatus = async (status: Status, userId: string) => {
        return await this.db.category.findMany({
            where: {
                videoCategory: {
                    some: {
                        video: {
                            status: status,
                            VideoRequest: {
                                some: {
                                    userId: userId,
                                },
                            },
                        },
                    },
                },
            },
        });
    };

    createVideoCategory = async (videoId: number, categoryId: string) => {
        try {
            const existingEntry = await this.db.videoCategory.findUnique({
                where: { videoId_categoryId: { videoId: videoId, categoryId: categoryId } },
            });

            if (!existingEntry) {
                return await this.db.videoCategory.create({
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

    updateMissingBoxArtUrls = async () => {
        try {
            logger.info("Updating missing box art");
            const categoriesWithMissingBoxArt = await this.db.category.findMany({
                where: {
                    boxArtUrl: "",
                },
            });
            for (const category of categoriesWithMissingBoxArt) {
                const fetchedGame = await this.twitchService.getGameDetail(category.id);
                await this.db.category.update({
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

    createStreamCategory = async (streamId: string, categoryId: string, prismaInstance: PrismaClient) => {
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

    createDownloadScheduleCategory = async (downloadScheduleId: number, categoryId: string) => {
        try {
            const existingEntry = await this.db.downloadScheduleCategory.findUnique({
                where: {
                    downloadScheduleId_categoryId: {
                        downloadScheduleId: downloadScheduleId,
                        categoryId: categoryId,
                    },
                },
            });
            if (!existingEntry) {
                return await this.db.downloadScheduleCategory.create({
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
}
