import express, { Router } from "express";
import * as downloadController from "../controllers/downloadController";
import { userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/user/:id", userAuthenticated, downloadController.scheduleUser);
router.get("/stream/:name", downloadController.downloadStream);
// router.get("/video/:id", downloadController.downloadVideo);

export default router;
