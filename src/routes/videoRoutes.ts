import express, { Router } from "express";
import * as videoController from "../controllers/videoController";

const router: Router = express.Router();

router.get("/play/:id", videoController.playVideo);
router.get("/all", videoController.getVideos);
router.get("/finished", videoController.getFinishedVideos);
router.get("/user/:id", videoController.getUserVideos);
router.get("/updazte/missing", videoController.generateMissingThumbnail);

export default router;
