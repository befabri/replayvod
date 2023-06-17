import express, { Router } from "express";
import * as downloadController from "../controllers/downloadController";
import { userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/user/:id", userAuthenticated, downloadController.scheduleUser);

export default router;
