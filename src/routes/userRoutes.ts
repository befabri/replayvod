import express, { Router } from "express";
import * as userController from "../controllers/userController";
import { userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/follows", userAuthenticated, userController.followList);

export default router;
