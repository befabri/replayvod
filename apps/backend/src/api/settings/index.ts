import { PrismaClient } from "@prisma/client/extension";
import { SettingsHandler } from "./settings.handler";
import { SettingsRepository } from "./settings.repository";

export type SettingsModule = {
    repository: SettingsRepository;
    handler: SettingsHandler;
};

export const settingsModule = (db: PrismaClient): SettingsModule => {
    const repository = new SettingsRepository(db);
    const handler = new SettingsHandler();

    return {
        repository,
        handler,
    };
};
