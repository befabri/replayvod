import { logger as rootLogger } from "../app";
const logger = rootLogger.child({ domain: "service", service: "tag" });
import { PrismaClient, Tag } from "@prisma/client";

export class TagService {
    constructor(private db: PrismaClient) {}

    createTag = async (tag: Tag) => {
        try {
            const existingTag = await this.db.tag.findUnique({
                where: { name: tag.name },
            });
            if (!existingTag) {
                return await this.db.tag.create({
                    data: tag,
                });
            } else {
                logger.debug("Tag already exists: %s", tag.name);
                return existingTag;
            }
        } catch (error) {
            logger.error("Error creating tag: %s", error);
            throw error;
        }
    };

    createMultipleTags = async (tags: Tag[]) => {
        try {
            const createTagPromises = tags.map((tag) => this.createTag(tag));
            const results = await Promise.all(createTagPromises);
            return results;
        } catch (error) {
            logger.error("Error creating multiple tags: %s", error);
            throw error;
        }
    };

    getAllTags = async () => {
        return this.db.tag.findMany();
    };

    getTag = async (id: string) => {
        return this.db.tag.findUnique({ where: { name: id } });
    };

    addVideoTag = async (videoId: number, tagId: string) => {
        return await this.db.videoTag.create({
            data: {
                videoId: videoId,
                tagId: tagId,
            },
        });
    };

    addStreamTag = async (streamId: string, tagId: string) => {
        return await this.db.streamTag.create({
            data: {
                streamId: streamId,
                tagId: tagId,
            },
        });
    };

    addDownloadScheduleTag = async (downloadScheduleId: number, tagId: string) => {
        return await this.db.downloadScheduleTag.create({
            data: {
                downloadScheduleId: downloadScheduleId,
                tagId: tagId,
            },
        });
    };

    addAllVideoTags = async (tags: { tagId: string }[], videoId: number) => {
        try {
            const data = tags.map((tag) => ({
                videoId: videoId,
                tagId: tag.tagId,
            }));

            await this.db.videoTag.createMany({
                data: data,
                skipDuplicates: true,
            });
            return data;
        } catch (error) {
            logger.error("Error adding/updating multiple videoTags %s", error);
        }
    };

    createAllDownloadScheduleTags = async (tags: Tag[], downloadScheduleId: number) => {
        try {
            const createTagPromises = tags.map((tag) => this.createTag(tag));
            await Promise.all(createTagPromises);
            const data = tags.map((tag) => ({
                downloadScheduleId: downloadScheduleId,
                tagId: tag.name,
            }));
            await this.db.downloadScheduleTag.createMany({
                data: data,
                skipDuplicates: true,
            });
            return data;
        } catch (error) {
            logger.error("Error adding/updating multiple downloadScheduleTags %s", error);
        }
    };

    associateTagsWithStream = async (streamId: string, tags: string[]): Promise<void> => {
        for (const tag of tags) {
            await this.db.streamTag.upsert({
                where: { streamId_tagId: { streamId, tagId: tag } },
                create: {
                    streamId,
                    tagId: tag,
                },
                update: {},
            });
        }
    };

    createMultipleStreamTags = async (tags: { tagId: string }[], streamId: string, tx: PrismaClient) => {
        try {
            const data = tags.map((tag) => ({
                streamId: streamId,
                tagId: tag.tagId,
            }));

            await tx.streamTag.createMany({
                data: data,
                skipDuplicates: true,
            });
            return data;
        } catch (error) {
            logger.error("Error adding/updating multiple streamTags %s", error);
            throw error;
        }
    };
}
