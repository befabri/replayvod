import express, { Router } from "express";
import * as userController from "../controllers/userController";
import { isUserWhitelisted, userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/me/followedstreams", isUserWhitelisted, userAuthenticated, userController.getUserFollowedStreams);
router.get("/:id", isUserWhitelisted, userAuthenticated, userController.getUserDetail);
router.put("/:id", isUserWhitelisted, userAuthenticated, userController.updateUserDetail);
router.get("/", isUserWhitelisted, userAuthenticated, userController.getMultipleUserDetailsFromDB);
router.post("/", isUserWhitelisted, userAuthenticated, userController.fetchAndStoreUserDetails);
router.get("/me/followedchannels", isUserWhitelisted, userAuthenticated, userController.getUserFollowedChannels);
router.get("/update/users", isUserWhitelisted, userAuthenticated, userController.updateUsers);
router.get("/name/:name", isUserWhitelisted, userAuthenticated, userController.getUserDetailByName);

export default router;
