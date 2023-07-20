import { Request, Response } from "express";
import axios from "axios";

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

export async function refreshToken(req: Request, res: Response) {
  if (req.session?.passport?.user?.refreshToken) {
    const refreshToken = req.session.passport.user.refreshToken;
    const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;
    const TWITCH_SECRET = process.env.TWITCH_SECRET;

    try {
      const response = await axios({
        method: "post",
        url: "https://id.twitch.tv/oauth2/token",
        params: {
          grant_type: "refresh_token",
          refresh_token: refreshToken,
          client_id: TWITCH_CLIENT_ID,
          client_secret: TWITCH_SECRET,
        },
      });

      if (response.status === 200) {
        req.session.passport.user.accessToken = response.data.access_token;
        req.session.passport.user.refreshToken = response.data.refresh_token;

        res.status(200).json({ status: "Token refreshed" });
      } else {
        res.status(500).json({ error: "Failed to refresh token" });
      }
    } catch (error) {
      console.error(`Failed to refresh token: ${error}`);
      res.status(500).json({ error: "Failed to refresh token" });
    }
  } else {
    res.status(401).json({ error: "Unauthorized" });
  }
}
