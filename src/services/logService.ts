import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "logService" });

export const getLog = async (id: number) => {
    return prisma.log.findUnique({ where: { id: id } });
};

export const getAllLogs = async () => {
    return prisma.log.findMany();
};

export default {
    getLog,
    getAllLogs,
};
