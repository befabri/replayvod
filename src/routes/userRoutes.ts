import express, { Router } from "express";
import * as userController from "../controllers/userController";

const router: Router = express.Router();

router.get("/me/followedstreams", userController.getUserFollowedStreams);
router.get("/:id", userController.getUserDetail);
router.put("/:id", userController.updateUserDetail);
router.get("/", userController.getMultipleUserDetailsFromDB);
router.post("/", userController.fetchAndStoreUserDetails);
router.get("/me/followedchannels", userController.getUserFollowedChannels);
router.get("/update/users", userController.updateUsers);
router.get("/name/:name", userController.getUserDetailByName);

export default router;
