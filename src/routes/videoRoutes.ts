import express, { Router } from "express";
import * as videoController from "../controllers/videoController";

const router: Router = express.Router();

router.get("/play/:id", videoController.playVideo);
router.get("/all", videoController.getVideos);
router.get("/finished", videoController.getFinishedVideos);
router.get("/user/:id", videoController.getUserVideos);
router.get("/update/missing", videoController.generateMissingThumbnail);
router.get("/thumbnail/:login/:filename", videoController.getThumbnail);

export default router;
