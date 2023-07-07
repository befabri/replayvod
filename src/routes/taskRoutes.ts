import express, { Router } from "express";
import * as taskController from "../controllers/taskController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/tasks", isUserWhitelisted, userAuthenticated, taskController.getTasks);
router.get("/tasks/run/:id", isUserWhitelisted, userAuthenticated, taskController.runTask);
router.get("/tasks/:id", isUserWhitelisted, userAuthenticated, taskController.getTask);

export default router;
