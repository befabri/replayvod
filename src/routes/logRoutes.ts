import express, { Router } from "express";
import * as logController from "../controllers/logController";

const router: Router = express.Router();

router.get("/files/:id", logController.getLog);
router.get("/files", logController.getLogs);

export default router;
