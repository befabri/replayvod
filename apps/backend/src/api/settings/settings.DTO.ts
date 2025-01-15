import { logger as rootLogger } from "../../app";
import { Settings } from "@prisma/client";

const logger = rootLogger.child({ domain: "settings", service: "dto" });

export interface SettingsDTO {
    timeZone: string;
    dateTimeFormat: string;
}

export const transformSettings = async (
    settings: SettingsDTO,
    userId: string
): Promise<{ settings: Settings }> => {
    try {
        const transformedSettings = {
            userId,
            timeZone: settings.timeZone,
            dateTimeFormat: settings.dateTimeFormat,
        };
        return {
            settings: transformedSettings,
        };
    } catch (error) {
        if (error instanceof Error) {
            logger.error(`Error transforming settings: ${error.message}`);
        } else {
            logger.error(`An unknown error occurred while transforming settings`);
        }
        throw error;
    }
};
