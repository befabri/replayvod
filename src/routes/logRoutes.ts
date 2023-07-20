import express, { Router } from "express";
import * as logController from "../controllers/logController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/files/:id", isUserWhitelisted, userAuthenticated, logController.getLog);
router.get("/files", isUserWhitelisted, userAuthenticated, logController.getLogs);

export default router;
