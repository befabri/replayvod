import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import * as taskService from "./task";

interface Params extends RouteGenericInterface {
    Params: {
        id: string;
    };
}

export const getTask = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    try {
        const taskId = req.params.id;
        const task = await taskService.getTask(taskId);
        reply.send(task);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};

export const getTasks = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const tasks = await taskService.getAllTasks();
        reply.send(tasks);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};

export const runTask = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    try {
        const taskId = req.params.id;
        const taskResult = await taskService.runTask(taskId);
        reply.send(taskResult);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};
