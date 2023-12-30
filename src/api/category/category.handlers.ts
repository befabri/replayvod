import { FastifyRequest, FastifyReply } from "fastify";
import { categoryFeature } from ".";
import { Status } from "@prisma/client";

export const getCategories = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const categories = await categoryFeature.getAllCategories();
        reply.send(categories);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};

export const getVideosCategories = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const categories = await categoryFeature.getAllVideosCategories();
        reply.send(categories);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};

export const getVideosCategoriesDone = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const categories = await categoryFeature.getAllVideosCategoriesByStatus(Status.DONE);
        reply.send(categories);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};
