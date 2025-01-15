import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { Status } from "@prisma/client";

interface Params extends RouteGenericInterface {
    Params: {
        name: string;
    };
}

export class CategoryHandler {
    getCategories = async (req: FastifyRequest, reply: FastifyReply) => {
        try {
            const repository = req.server.category.repository;
            const categories = await repository.getAllCategories();
            reply.send(categories);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    getCategory = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const categoryName = req.params.name;
        const repository = req.server.category.repository;
        if (!categoryName) {
            return reply.status(400).send({ message: "Invalid category name" });
        }
        const category = await repository.getCategoryByName(categoryName);
        reply.send(category);
    };

    getVideosCategories = async (req: FastifyRequest, reply: FastifyReply) => {
        try {
            const repository = req.server.category.repository;
            const userRepository = req.server.user.repository;
            const userId = userRepository.getUserIdFromSession(req);
            if (!userId) {
                return reply.status(401).send({ message: "Unauthorized" });
            }
            const categories = await repository.getAllVideosCategories(userId);
            reply.send(categories);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    getVideosCategoriesDone = async (req: FastifyRequest, reply: FastifyReply) => {
        try {
            const userRepository = req.server.user.repository;
            const userId = userRepository.getUserIdFromSession(req);
            const repository = req.server.category.repository;
            if (!userId) {
                return reply.status(401).send({ message: "Unauthorized" });
            }
            const categories = await repository.getAllVideosCategoriesByStatus(Status.DONE, userId);
            reply.send(categories);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };
}
