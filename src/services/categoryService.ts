import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "categoryService" });
import { Category } from "@prisma/client";

export const addCategory = async (category: Category) => {
    try {
        return prisma.category.upsert({
            where: { id: category.id },
            update: {
                boxArtUrl: category.boxArtUrl,
                igdbId: category.igdbId,
                name: category.name,
            },
            create: category,
        });
    } catch (error) {
        logger.error("Error adding/updating category:", error);
        throw error;
    }
};

export const addAllCategories = async (categories: Category[]) => {
    try {
        const promises = categories.map((category) => addCategory(category));
        return Promise.all(promises);
    } catch (error) {
        logger.error("Error adding/updating multiple categories:", error);
        throw error;
    }
};

export const getAllCategories = async () => {
    return prisma.category.findMany();
};

export const getCategoryById = async (id: string) => {
    return prisma.category.findUnique({ where: { id: id } });
};

export const getCategoryByName = async (name: string) => {
    return prisma.category.findUnique({ where: { name: name } });
};

export default {
    addCategory,
    addAllCategories,
    getAllCategories,
    getCategoryById,
    getCategoryByName,
};
