import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";

interface Params extends RouteGenericInterface {
    Params: {
        id: string;
    };
}

export class TaskHandler {
    getTask = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        try {
            const repository = req.server.task.repository;
            const taskId = req.params.id;
            const task = await repository.getTask(taskId);
            reply.send(task);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    getTasks = async (req: FastifyRequest, reply: FastifyReply) => {
        try {
            const repository = req.server.task.repository;
            const tasks = await repository.getAllTasks();
            reply.send(tasks);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    runTask = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        try {
            const taskId = req.params.id;
            const repository = req.server.task.repository;
            const taskResult = await repository.runTask(taskId);
            reply.send(taskResult);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };
}
