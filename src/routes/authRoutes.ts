import express from "express";
import passport from "passport";
import * as authController from "../controllers/authController";
import dotenv from "dotenv";

dotenv.config();
const router = express.Router();
const REDIRECT_URL = process.env.REDIRECT_URL || "/";

router.get(
  "/twitch",
  passport.authenticate("twitch", { scope: ["user:read:email", "user:read:follows"] }),
  authController.handleTwitchAuth
);

router.get(
  "/twitch/callback",
  passport.authenticate("twitch", { successRedirect: REDIRECT_URL, failureRedirect: "https://google.com" }),
  authController.handleTwitchCallback
);

router.get("/check-session", authController.checkSession);

router.get("/user", authController.getUser);

router.get("/refresh", authController.refreshToken);

export default router;
