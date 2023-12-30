import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ domain: "channel", service: "titleService" });
import { PrismaClient, Title } from "@prisma/client";

const createTitle = async (title: Omit<Title, "id">) => {
    try {
        const existingTitle = await prisma.title.findUnique({
            where: { name: title.name },
        });
        if (!existingTitle) {
            return await prisma.title.create({
                data: title,
            });
        } else {
            logger.info("Title already exists: %s", title.name);
            return existingTitle;
        }
    } catch (error) {
        logger.error("Error creating title: %s", error);
        throw error;
    }
};

const createMultipleTitles = async (titles: Omit<Title, "id">[]) => {
    try {
        const createTitlePromises = titles.map((title) => createTitle(title));
        const results = await Promise.all(createTitlePromises);
        return results;
    } catch (error) {
        logger.error("Error creating multiple titles: %s", error);
        throw error;
    }
};

const getAllTitles = async () => {
    return prisma.title.findMany();
};

const getTitleById = async (id: number) => {
    return prisma.title.findUnique({ where: { id: id } });
};

const getTitleByName = async (name: string) => {
    return prisma.title.findUnique({ where: { name: name } });
};

const createVideoTitle = async (videoId: number, titleId: string) => {
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

const createStreamTitle = async (streamId: string, titleId: string, prismaInstance: PrismaClient = prisma) => {
    try {
        const existingEntry = await prismaInstance.streamTitle.findUnique({
            where: { streamId_titleId: { streamId: streamId, titleId: titleId } },
        });

        if (!existingEntry) {
            return await prismaInstance.streamTitle.create({
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
    createTitle,
    createMultipleTitles,
    getAllTitles,
    getTitleById,
    getTitleByName,
    createVideoTitle,
    createStreamTitle,
};
