import { logger as rootLogger } from "@app";
import { prisma } from "@server";
const logger = rootLogger.child({ domain: "channel", service: "titleService" });
import { Title } from "@prisma/client";

const MAX_RETRIES = 3;
const RETRY_DELAY = 1000;

export const addTitle = async (title: Omit<Title, "id">) => {
    try {
        return prisma.title.upsert({
            where: { name: title.name },
            update: title,
            create: title,
        });
    } catch (error) {
        logger.error("Error adding/updating title: %s", error);
        throw error;
    }
};

export const addAllTitles = async (titles: Omit<Title, "id">[]) => {
    let attempts = 0;
    const sortedTitles = titles.sort((a, b) => a.name.localeCompare(b.name));

    while (attempts < MAX_RETRIES) {
        try {
            await prisma.title.createMany({
                data: sortedTitles,
                skipDuplicates: true,
            });
            return sortedTitles;
        } catch (error) {
            if (error.message && error.message.includes("deadlock")) {
                logger.error(`Deadlock encountered while adding/updating titles (Attempt ${attempts + 1})`, {
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

export const getAllTitles = async () => {
    return prisma.title.findMany();
};

export const getTitleById = async (id: number) => {
    return prisma.title.findUnique({ where: { id: id } });
};

export const getTitleByName = async (name: string) => {
    return prisma.title.findUnique({ where: { name: name } });
};

const addVideoTitle = async (videoId: number, titleId: string) => {
    try {
        const existingEntry = await prisma.videoTitle.findUnique({
            where: { videoId_titleId: { videoId: videoId, titleId: titleId } },
        });

        if (!existingEntry) {
            return await prisma.videoTitle.create({
                data: {
                    videoId: videoId,
                    titleId: titleId,
                },
            });
        } else {
            return existingEntry;
        }
    } catch (error) {
        logger.error("Error adding/updating videoTitle: %s", error);
        throw error;
    }
};

const addStreamTitle = async (streamId: string, titleId: string) => {
    try {
        const existingEntry = await prisma.streamTitle.findUnique({
            where: { streamId_titleId: { streamId: streamId, titleId: titleId } },
        });

        if (!existingEntry) {
            return await prisma.streamTitle.create({
                data: {
                    streamId: streamId,
                    titleId: titleId,
                },
            });
        } else {
            return existingEntry;
        }
    } catch (error) {
        logger.error("Error adding/updating streamTitle: %s", error);
        throw error;
    }
};

export default {
    addTitle,
    addAllTitles,
    getAllTitles,
    getTitleById,
    getTitleByName,
    addVideoTitle,
    addStreamTitle,
};
