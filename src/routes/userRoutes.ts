import express, { Router } from "express";
import * as userController from "../controllers/userController";
import { userAuthenticated } from "../middlewares/authMiddleware";

const router: Router = express.Router();

router.get("/me/followedstreams", userAuthenticated, userController.getUserFollowedStreams);
router.get("/:id", userAuthenticated, userController.getUserDetail);
router.put("/:id", userAuthenticated, userController.updateUserDetail);
router.get("/", userAuthenticated, userController.getMultipleUserDetailsFromDB);
router.post("/", userAuthenticated, userController.fetchAndStoreUserDetails);
router.get("/me/followedchannels", userAuthenticated, userController.getUserFollowedChannels);
router.get("/update/users", userAuthenticated, userController.updateUsers);
router.get("/name/:name", userAuthenticated, userController.getUserDetailByName);

export default router;
