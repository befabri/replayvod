import { Request, Response } from "express";
import ScheduleService from "../services/scheduleService";

const scheduleService = new ScheduleService();

export const getTasks = async (req: Request, res: Response) => {
    try {
        const tasks = await scheduleService.getAllTasks();
        res.json(tasks);
    } catch (error) {
        res.status(500).send("Internal server error");
    }
};

export const getTask = async (req: Request, res: Response) => {
    try {
        const taskId = req.params.id;
        const task = await scheduleService.getTask(taskId);
        res.json(task);
    } catch (error) {
        res.status(500).send("Internal server error");
    }
};

export const runTask = async (req: Request, res: Response) => {
    try {
        const taskId = req.params.id;
        console.log("-> ", taskId);
        const taskResult = await scheduleService.runTask(taskId);
        res.json(taskResult);
    } catch (error) {
        console.log("Errorr task ");
        res.status(500).send("Internal server error");
    }
};
