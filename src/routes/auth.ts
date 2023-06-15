import express from "express";
import passport from "passport";

const router = express.Router();
const REDIRECT_URL = "http://localhost:5173/vod";

router.get("/twitch", passport.authenticate("twitch", { scope: "user_read" }));

router.get(
  "/twitch/callback",
  passport.authenticate("twitch", { successRedirect: REDIRECT_URL, failureRedirect: "/" })
);

router.get("/check-session", (req, res) => {
  if (req.session?.passport?.user) {
    res.status(200).json({ status: "authenticated" });
  } else {
    res.status(200).json({ status: "not authenticated" });
  }
});

router.get("/user", (req, res) => {
  if (req.session?.passport?.user) {
    res.json(req.session.passport.user);
  } else {
    res.status(401).json({ error: "Unauthorized" });
  }
});

export default router;
