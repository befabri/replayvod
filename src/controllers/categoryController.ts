import { FastifyRequest, FastifyReply } from "fastify";
import { categoryService } from "../services";

export const getCategories = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const categories = await categoryService.getAllCategories();
        reply.send(categories);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};
