import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "tagService" });
import { Tag } from "@prisma/client";

export const addTag = async (tag: Tag) => {
    try {
        return prisma.tag.upsert({
            where: { name: tag.name },
            update: {},
            create: tag,
        });
    } catch (error) {
        logger.error("Error adding/updating tag:", error);
        throw error;
    }
};

export const addAllTags = async (tags: Tag[]) => {
    try {
        const promises = tags.map((tag) => addTag(tag));
        return Promise.all(promises);
    } catch (error) {
        logger.error("Error adding/updating multiple tags:", error);
        throw error;
    }
};

export const getAllTags = async () => {
    return prisma.tag.findMany();
};

export const getTag = async (id: string) => {
    return prisma.tag.findUnique({ where: { name: id } });
};

export default {
    addTag,
    addAllTags,
    getAllTags,
    getTag,
};
