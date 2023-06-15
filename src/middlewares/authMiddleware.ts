import { Request, Response, NextFunction } from "express";

export function userAuthenticated(req: Request, res: Response, next: NextFunction) {
  if (req.session?.passport?.user) {
    next();
  } else {
    res.status(401).json({ error: "Unauthorized" });
  }
}
