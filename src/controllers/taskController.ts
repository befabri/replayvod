import { Request, Response } from "express";
import { taskService } from "../services";

export const getTasks = async (req: Request, res: Response) => {
    try {
        const tasks = await taskService.getAllTasks();
        res.json(tasks);
    } catch (error) {
        res.status(500).send("Internal server error");
    }
};

export const getTask = async (req: Request, res: Response) => {
    try {
        const taskId = req.params.id;
        const task = await taskService.getTask(taskId);
        res.json(task);
    } catch (error) {
        res.status(500).send("Internal server error");
    }
};

export const runTask = async (req: Request, res: Response) => {
    try {
        const taskId = req.params.id;
        const taskResult = await taskService.runTask(taskId);
        res.json(taskResult);
    } catch (error) {
        res.status(500).send("Internal server error");
    }
};
