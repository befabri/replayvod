import { Request, Response, NextFunction } from "express";
import dotenv from "dotenv";

dotenv.config();

const WHITELISTED_USER_IDS: string[] = process.env.WHITELISTED_USER_IDS?.split(",") || [];
const IS_WHITELIST_ENABLED: boolean = process.env.IS_WHITELIST_ENABLED?.toLowerCase() === "true";

export function isUserWhitelisted(req: Request, res: Response, next: NextFunction) {
  const userID = req.session?.passport?.user?.twitchId;
  if (!IS_WHITELIST_ENABLED || (userID && WHITELISTED_USER_IDS.includes(userID))) {
    next();
  } else {
    res.status(403).json({ error: "Forbidden, you're not on the whitelist." });
  }
}

export function userAuthenticated(req: Request, res: Response, next: NextFunction) {
  if (req.session?.passport?.user) {
    next();
  } else {
    res.status(401).json({ error: "Unauthorized" });
  }
}
