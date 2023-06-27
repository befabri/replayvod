import express, { Router } from "express";
import * as videoController from "../controllers/videoController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/play/:id", isUserWhitelisted, userAuthenticated, videoController.playVideo);
router.get("/all", isUserWhitelisted, userAuthenticated, videoController.getVideos);
router.get("/finished", isUserWhitelisted, userAuthenticated, videoController.getFinishedVideos);
router.get("/user/:id", isUserWhitelisted, userAuthenticated, videoController.getUserVideos);
router.get("/update/missing", isUserWhitelisted, userAuthenticated, videoController.generateMissingThumbnail);
router.get("/thumbnail/:login/:filename", isUserWhitelisted, userAuthenticated, videoController.getThumbnail);

export default router;
