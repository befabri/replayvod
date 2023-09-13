import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "titleService" });
import { Title } from "@prisma/client";

export const addTitle = async (title: Title) => {
    try {
        return prisma.title.upsert({
            where: { id: title.id },
            update: {
                id: title.id,
                name: title.name,
            },
            create: title,
        });
    } catch (error) {
        logger.error("Error adding/updating title: %s", error);
        throw error;
    }
};

export const addAllTitles = async (titles: Title[]) => {
    try {
        const promises = titles.map((title) => addTitle(title));
        return Promise.all(promises);
    } catch (error) {
        logger.error("Error adding/updating multiple titles");
        throw error;
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

const addVideoTitle = async (videoId, titleId) => {
    try {
        return await prisma.videoTitle.create({
            data: {
                videoId: videoId,
                titleId: titleId,
            },
        });
    } catch (error) {
        logger.error("Error adding/updating videoTitle: %s", error);
        throw error;
    }
};

const addStreamTitle = async (streamId, titleId) => {
    try {
        return await prisma.streamTitle.create({
            data: {
                streamId: streamId,
                titleId: titleId,
            },
        });
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
