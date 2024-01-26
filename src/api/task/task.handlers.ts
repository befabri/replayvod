import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { taskFeature } from ".";

interface Params extends RouteGenericInterface {
    Params: {
        id: string;
    };
}

export const getTask = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    try {
        const taskId = req.params.id;
        const task = await taskFeature.getTask(taskId);
        reply.send(task);
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const getTasks = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const tasks = await taskFeature.getAllTasks();
        reply.send(tasks);
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const runTask = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    try {
        const taskId = req.params.id;
        const taskResult = await taskFeature.runTask(taskId);
        reply.send(taskResult);
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};
