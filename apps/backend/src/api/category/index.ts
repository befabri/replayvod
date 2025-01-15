import { PrismaClient } from "@prisma/client/extension";
import { TwitchService } from "../../services/service.twitch";
import { CategoryHandler } from "./category.handler";
import { CategoryRepository } from "./category.repository";

export type CategoryModule = {
    repository: CategoryRepository;
    handler: CategoryHandler;
};

export const categoryModule = (db: PrismaClient, twitchService: TwitchService): CategoryModule => {
    const repository = new CategoryRepository(db, twitchService);
    const handler = new CategoryHandler();

    return {
        repository,
        handler,
    };
};
