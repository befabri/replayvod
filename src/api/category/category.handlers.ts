import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { categoryFeature } from ".";
import { Status } from "@prisma/client";
import { userFeature } from "../user";

interface Params extends RouteGenericInterface {
    Params: {
        name: string;
    };
}

export const getCategories = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const categories = await categoryFeature.getAllCategories();
        reply.send(categories);
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const getCategory = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const categoryName = req.params.name;
    if (!categoryName) {
        return reply.status(400).send({ message: "Invalid category name" });
    }
    const category = await categoryFeature.getCategoryByName(categoryName);
    reply.send(category);
};

export const getVideosCategories = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const userId = userFeature.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const categories = await categoryFeature.getAllVideosCategories(userId);
        reply.send(categories);
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const getVideosCategoriesDone = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const userId = userFeature.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const categories = await categoryFeature.getAllVideosCategoriesByStatus(Status.DONE, userId);
        reply.send(categories);
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};
