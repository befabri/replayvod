import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "tagService" });
import { Tag } from "@prisma/client";

const MAX_RETRIES = 3;
const RETRY_DELAY = 1000;

export const addTag = async (tag: Tag) => {
    try {
        return prisma.tag.upsert({
            where: { name: tag.name },
            update: {},
            create: tag,
        });
    } catch (error) {
        logger.error("Tag data causing error: %s", tag.name);
        error.failedTagName = tag.name;
        throw error;
    }
};

export const addAllTags = async (tags: Tag[]) => {
    let attempts = 0;
    const sortedTags = tags.sort((a, b) => a.name.localeCompare(b.name));

    while (attempts < MAX_RETRIES) {
        try {
            await prisma.tag.createMany({
                data: sortedTags,
                skipDuplicates: true,
            });
            return sortedTags;
        } catch (error) {
            if (error.message && error.message.includes("deadlock")) {
                logger.error(`Deadlock encountered while adding/updating tags (Attempt ${attempts + 1})`, {
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
        const data = tags.map((tag) => ({
            videoId: videoId,
            tagId: tag.tagId,
        }));

        await prisma.videoTag.createMany({
            data: data,
            skipDuplicates: true,
        });
        return data;
    } catch (error) {
        logger.error("Error adding/updating multiple videoTags", { error });
    }
};

export const addAllStreamTags = async (tags: { tagId: string }[], streamId: string) => {
    try {
        const data = tags.map((tag) => ({
            streamId: streamId,
            tagId: tag.tagId,
        }));

        await prisma.streamTag.createMany({
            data: data,
            skipDuplicates: true,
        });
        return data;
    } catch (error) {
        logger.error("Error adding/updating multiple streamTags", { error });
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
