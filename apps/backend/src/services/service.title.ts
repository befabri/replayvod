import { logger as rootLogger } from "../app";
import { PrismaClient, Title } from "@prisma/client";
const logger = rootLogger.child({ domain: "service", service: "title" });

export class TitleService {
    constructor(private db: PrismaClient) {}

    createTitle = async (title: Omit<Title, "id">) => {
        try {
            const existingTitle = await this.db.title.findUnique({
                where: { name: title.name },
            });
            if (!existingTitle) {
                return await this.db.title.create({
                    data: title,
                });
            } else {
                logger.debug("Title already exists: %s", title.name);
                return existingTitle;
            }
        } catch (error) {
            logger.error("Error creating title: %s", error);
            throw error;
        }
    };

    createMultipleTitles = async (titles: Omit<Title, "id">[]) => {
        try {
            const createTitlePromises = titles.map((title) => this.createTitle(title));
            const results = await Promise.all(createTitlePromises);
            return results;
        } catch (error) {
            logger.error("Error creating multiple titles: %s", error);
            throw error;
        }
    };

    getAllTitles = async () => {
        return this.db.title.findMany();
    };

    getTitleById = async (id: number) => {
        return this.db.title.findUnique({ where: { id: id } });
    };

    getTitleByName = async (name: string) => {
        return this.db.title.findUnique({ where: { name: name } });
    };

    createVideoTitle = async (videoId: number, titleId: string) => {
        try {
            const existingEntry = await this.db.videoTitle.findUnique({
                where: { videoId_titleId: { videoId: videoId, titleId: titleId } },
            });

            if (!existingEntry) {
                return await this.db.videoTitle.create({
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

    createStreamTitle = async (streamId: string, titleId: string, tx: PrismaClient) => {
        try {
            const existingEntry = await tx.streamTitle.findUnique({
                where: { streamId_titleId: { streamId: streamId, titleId: titleId } },
            });

            if (!existingEntry) {
                return await tx.streamTitle.create({
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
}
