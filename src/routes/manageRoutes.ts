import express, { Router } from "express";
import * as twitchAPIController from "../controllers/twitchAPIController";

const router: Router = express.Router();

router.get("/update/games", twitchAPIController.fetchAndSaveGames);

export default router;
