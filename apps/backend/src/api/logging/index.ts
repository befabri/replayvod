import { PrismaClient } from "@prisma/client/extension";
import { LoggingHandler } from "./logging.handlers";
import { LoggingRepository } from "./logging.repository";

export type LoggingModule = {
    repository: LoggingRepository;
    handler: LoggingHandler;
};

export const loggingModule = (db: PrismaClient): LoggingModule => {
    const repository = new LoggingRepository(db);
    const handler = new LoggingHandler();

    return {
        repository,
        handler,
    };
};
