import express, { Router } from "express";
import * as userController from "../controllers/userController";
import { userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/follows", userAuthenticated, userController.followList);
router.get("/users/:id", userAuthenticated, userController.getUserDetail);
router.put("/users/:id", userAuthenticated, userController.updateUserDetail);
router.get("/users", userAuthenticated, userController.getMultipleUserDetailsFromDB);
router.post("/fetchAndStoreUsers", userAuthenticated, userController.fetchAndStoreUserDetails);

export default router;
