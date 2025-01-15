import fp from "fastify-plugin";
import { FastifyInstance, FastifyPluginOptions } from "fastify";
import { logger as rootLogger } from "../app";
import { PrismaClient } from "@prisma/client";

const logger = rootLogger.child({ domain: "plugins", service: "prisma" });

export const prismaPlugin = fp(
    (fastify: FastifyInstance, _options: FastifyPluginOptions, done) => {
        if (!fastify.prisma) {
            const prisma = new PrismaClient();
            fastify.decorate("prisma", prisma);
            fastify.addHook("onClose", (fastify, done) => {
                if (fastify.prisma === prisma) {
                    logger.info("Closing Prisma connection...");
                    try {
                        fastify.prisma.$disconnect();
                        logger.info("Prisma connection closed successfully.");
                    } catch (error) {
                        logger.error("Error disconnecting Prisma:", error);
                    } finally {
                        done();
                    }
                } else {
                    done();
                }
            });
        } else {
            logger.warn("Prisma plugin is already registered.");
        }
        done();
    },
    { name: "fastify-prisma" }
);
