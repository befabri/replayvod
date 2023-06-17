import { Request, Response } from "express";

export function handleTwitchAuth(req: Request, res: Response) {}

export function handleTwitchCallback(req: Request, res: Response) {}

export function checkSession(req: Request, res: Response) {
  if (req.session?.passport?.user) {
    res.status(200).json({ status: "authenticated" });
  } else {
    res.status(200).json({ status: "not authenticated" });
  }
}

export function getUser(req: Request, res: Response) {
  if (req.session?.passport?.user) {
    const { accessToken, refreshToken, ...user } = req.session.passport.user;
    res.json(user);
  } else {
    res.status(401).json({ error: "Unauthorized" });
  }
}
