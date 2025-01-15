import { PrismaClient, Settings } from "@prisma/client";
import { logger as rootLogger } from "../../app";

const logger = rootLogger.child({ domain: "settings", service: "repository" });

export class SettingsRepository {
    constructor(private db: PrismaClient) {}

    addSettings = async (settings: Settings) => {
        try {
            return this.db.settings.upsert({
                where: { userId: settings.userId },
                update: { ...settings },
                create: { ...settings },
            });
        } catch (error) {
            logger.error("Error adding/updating settings: %s", error);
            throw error;
        }
    };

    getSettings = async (userId: string) => {
        return this.db.settings.findUnique({ where: { userId: userId } });
    };
}
