import express, { Router } from "express";
import * as videoController from "../controllers/videoController";
import { userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/play/:id", userAuthenticated, videoController.playVideo);
router.get("/all", userAuthenticated, videoController.getVideos);

export default router;
