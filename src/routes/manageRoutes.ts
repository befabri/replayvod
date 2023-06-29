import express, { Router } from "express";
import * as twitchAPIController from "../controllers/twitchAPIController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/update/games", isUserWhitelisted, userAuthenticated, twitchAPIController.fetchAndSaveGames);
router.get("/eventsub/subscriptions", isUserWhitelisted, userAuthenticated, twitchAPIController.getSubscriptions);

export default router;
