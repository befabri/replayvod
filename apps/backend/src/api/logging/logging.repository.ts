import { PrismaClient } from "@prisma/client";

export class LoggingRepository {
    constructor(private db: PrismaClient) {}
    getLog = async (id: number) => {
        return this.db.log.findUnique({ where: { id: id } });
    };

    getAllLogs = async () => {
        return this.db.log.findMany();
    };

    getDomain = async (id: number) => {
        return this.db.eventLog.findUnique({ where: { id: id } });
    };

    getAllDomains = async () => {
        return this.db.eventLog.findMany();
    };
}
