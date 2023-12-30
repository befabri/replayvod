import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ domain: "channel", service: "tagService" });
import { PrismaClient, Tag } from "@prisma/client";

const createTag = async (tag: Tag) => {
    try {
        const existingTag = await prisma.tag.findUnique({
            where: { name: tag.name },
        });
        if (!existingTag) {
            return await prisma.tag.create({
                data: tag,
            });
        } else {
            logger.info("Tag already exists: %s", tag.name);
            return existingTag;
        }
    } catch (error) {
        logger.error("Error creating tag: %s", error);
        throw error;
    }
};

const createMultipleTags = async (tags: Tag[]) => {
    try {
        const createTagPromises = tags.map((tag) => createTag(tag));
        const results = await Promise.all(createTagPromises);
        return results;
    } catch (error) {
        logger.error("Error creating multiple tags: %s", error);
        throw error;
    }
};

const getAllTags = async () => {
    return prisma.tag.findMany();
};

const getTag = async (id: string) => {
    return prisma.tag.findUnique({ where: { name: id } });
};

const addVideoTag = async (videoId: number, tagId: string) => {
    return await prisma.videoTag.create({
        data: {
            videoId: videoId,
            tagId: tagId,
        },
    });
};

const addStreamTag = async (streamId: string, tagId: string) => {
    return await prisma.streamTag.create({
        data: {
            streamId: streamId,
            tagId: tagId,
        },
    });
};

const addDownloadScheduleTag = async (downloadScheduleId: number, tagId: string) => {
    return await prisma.downloadScheduleTag.create({
        data: {
            downloadScheduleId: downloadScheduleId,
            tagId: tagId,
        },
    });
};

const addAllVideoTags = async (tags: { tagId: string }[], videoId: number) => {
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
        logger.error("Error adding/updating multiple videoTags %s", error);
    }
};

const createAllDownloadScheduleTags = async (tags: { tagId: string }[], downloadScheduleId: number) => {
    try {
        const data = tags.map((tag) => ({
            downloadScheduleId: downloadScheduleId,
            tagId: tag.tagId,
        }));

        await prisma.downloadScheduleTag.createMany({
            data: data,
            skipDuplicates: true,
        });
        return data;
    } catch (error) {
        logger.error("Error adding/updating multiple downloadScheduleTags %s", error);
    }
};

const associateTagsWithStream = async (streamId: string, tags: string[]): Promise<void> => {
    for (const tag of tags) {
        await prisma.streamTag.upsert({
            where: { streamId_tagId: { streamId, tagId: tag } },
            create: {
                streamId,
                tagId: tag,
            },
            update: {},
        });
    }
};

export const createMultipleStreamTags = async (
    tags: { tagId: string }[],
    streamId: string,
    prismaInstance: PrismaClient = prisma
) => {
    try {
        const data = tags.map((tag) => ({
            streamId: streamId,
            tagId: tag.tagId,
        }));

        await prismaInstance.streamTag.createMany({
            data: data,
            skipDuplicates: true,
        });
        return data;
    } catch (error) {
        logger.error("Error adding/updating multiple streamTags %s", error);
        throw error;
    }
};

export default {
    createTag,
    createMultipleTags,
    getAllTags,
    getTag,
    addVideoTag,
    addStreamTag,
    addAllVideoTags,
    addDownloadScheduleTag,
    createAllDownloadScheduleTags,
    associateTagsWithStream,
    createMultipleStreamTags,
};
