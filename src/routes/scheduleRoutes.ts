import express, { Router } from "express";
import * as scheduleController from "../controllers/scheduleController";

const router: Router = express.Router();

router.get("/tasks", scheduleController.getTasks);
router.get("/tasks/run/:id", scheduleController.runTask);
router.get("/tasks/:id", scheduleController.getTask);

export default router;
