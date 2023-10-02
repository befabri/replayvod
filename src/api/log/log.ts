import { logger as rootLogger } from "../../app";
import { prisma } from "../../server";

export const getLog = async (id: number) => {
    return prisma.log.findUnique({ where: { id: id } });
};

export const getAllLogs = async () => {
    return prisma.log.findMany();
};

export const getDomain = async (id: number) => {
    return prisma.eventLog.findUnique({ where: { id: id } });
};

export const getAllDomains = async () => {
    return prisma.eventLog.findMany();
};
