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
        reply.status(500).send("Internal server error");
    }
};

export const getCategory = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const categoryName = req.params.name;
    if (!categoryName) {
        reply.status(400).send("Invalid category name");
        return;
    }
    const category = await categoryFeature.getCategoryByName(categoryName);
    reply.send(category);
};

export const getVideosCategories = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const userId = userFeature.getUserIdFromSession(req);
        if (!userId) {
            reply.status(401).send("Unauthorized");
            return;
        }
        const categories = await categoryFeature.getAllVideosCategories(userId);
        reply.send(categories);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};

export const getVideosCategoriesDone = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const userId = userFeature.getUserIdFromSession(req);
        if (!userId) {
            reply.status(401).send("Unauthorized");
            return;
        }
        const categories = await categoryFeature.getAllVideosCategoriesByStatus(Status.DONE, userId);
        reply.send(categories);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};
