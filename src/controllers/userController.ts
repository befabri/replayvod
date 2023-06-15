import axios from "axios";
import { Request, Response } from "express";
const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;

export const followList = async (req: Request, res: Response) => {
  if (!req.session?.passport?.user) {
    res.status(401).send("Unauthorized");
    return;
  }
  const userId = req.session.passport.user.data[0].id;
  const accessToken = req.session.passport.user.accessToken;
  if (userId == undefined) {
    res.status(500).send("Error fetching followed streams");
  }
  try {
    const response = await axios({
      method: "get",
      url: `https://api.twitch.tv/helix/streams/followed?user_id=${userId}`,
      headers: {
        Authorization: `Bearer ${accessToken}`,
        "Client-Id": TWITCH_CLIENT_ID,
      },
    });

    console.log(response.data);
    res.json(response.data);
  } catch (error) {
    console.error("Error fetching followed streams:", error);
    res.status(500).send("Error fetching followed streams");
  }
};
