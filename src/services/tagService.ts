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

export const addVideoTag = async (videoId: number, tagId: string) => {
    return await prisma.videoTag.create({
        data: {
            videoId: videoId,
            tagId: tagId,
        },
    });
};

export const addStreamTag = async (streamId: string, tagId: string) => {
    return await prisma.streamTag.create({
        data: {
            streamId: streamId,
            tagId: tagId,
        },
    });
};

export const addAllVideoTags = async (tags: { tagId: string }[], videoId: number) => {
    try {
        const promises = tags.map((tag) => addVideoTag(videoId, tag.tagId));
        return Promise.all(promises);
    } catch (error) {
        logger.error("Error adding/updating multiple tags:", error);
        throw error;
    }
};

export const addAllStreamTags = async (tags: { tagId: string }[], streamId: string) => {
    try {
        const promises = tags.map((tag) => addStreamTag(streamId, tag.tagId));
        return Promise.all(promises);
    } catch (error) {
        logger.error("Error adding/updating multiple tags:", error);
        throw error;
    }
};

export default {
    addTag,
    addAllTags,
    getAllTags,
    getTag,
    addVideoTag,
    addStreamTag,
    addAllVideoTags,
    addAllStreamTags,
};
