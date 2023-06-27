import express, { Router } from "express";
import * as scheduleController from "../controllers/scheduleController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/tasks", isUserWhitelisted, userAuthenticated, scheduleController.getTasks);
router.get("/tasks/run/:id", isUserWhitelisted, userAuthenticated, scheduleController.runTask);
router.get("/tasks/:id", isUserWhitelisted, userAuthenticated, scheduleController.getTask);

export default router;
