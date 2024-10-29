import { Settings } from "@prisma/client";
import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";

const logger = rootLogger.child({ domain: "settings", service: "settingsService" });

export const addSettings = async (settings: Settings) => {
    try {
        return prisma.settings.upsert({
            where: { userId: settings.userId },
            update: { ...settings },
            create: { ...settings },
        });
    } catch (error) {
        logger.error("Error adding/updating settings: %s", error);
        throw error;
    }
};

export const getSettings = async (userId: string) => {
    return prisma.settings.findUnique({ where: { userId: userId } });
};
