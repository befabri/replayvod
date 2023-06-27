import express, { Router } from "express";
import * as downloadController from "../controllers/downloadController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/user/:id", isUserWhitelisted, userAuthenticated, downloadController.scheduleUser);
router.get("/stream/:id", isUserWhitelisted, userAuthenticated, downloadController.downloadStream);
router.post("/channels", isUserWhitelisted, userAuthenticated, downloadController.scheduleDownload);
router.get("/status/:id", isUserWhitelisted, userAuthenticated, downloadController.getJobStatus);
// router.get("/video/:id", downloadController.downloadVideo);

export default router;
