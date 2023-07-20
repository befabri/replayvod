import express, { Router } from "express";
import * as downloadController from "../controllers/downloadController";

const router: Router = express.Router();

router.get("/user/:id", downloadController.scheduleUser);
router.get("/stream/:id", downloadController.downloadStream);
router.post("/channels", downloadController.scheduleDownload);
router.get("/status/:id", downloadController.getJobStatus);
// router.get("/video/:id", downloadController.downloadVideo);

export default router;
