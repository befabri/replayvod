import express from "express";
import passport from "passport";
import * as authController from "../controllers/authController";

const router = express.Router();
const REDIRECT_URL = "http://localhost:5173/";

router.get(
  "/twitch",
  passport.authenticate("twitch", { scope: ["user:read:email", "user:read:follows"] }),
  authController.handleTwitchAuth
);

router.get(
  "/twitch/callback",
  passport.authenticate("twitch", { successRedirect: REDIRECT_URL, failureRedirect: "/" }),
  authController.handleTwitchCallback
);

router.get("/check-session", authController.checkSession);

router.get("/user", authController.getUser);

router.get("/refresh", authController.refreshToken);

export default router;
