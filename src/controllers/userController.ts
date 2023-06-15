import axios from "axios";
import { Request, Response } from "express";
import { connect, getDbInstance } from "../models/db";

const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;

import { v4 as uuidv4 } from "uuid";

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
    const db = await getDbInstance();
    const collection = db.collection("followedStreams");
    const fetchLogCollection = db.collection("fetchLog");
    const fetchLog = await fetchLogCollection.findOne({ userId: userId }, { sort: { fetchedAt: -1 } });
    if (fetchLog && fetchLog.fetchedAt > new Date(Date.now() - 5 * 60 * 1000)) {
      const streams = await collection.find({ fetchId: fetchLog.fetchId }).toArray();
      res.json(streams);
      return;
    }
    const fetchId = uuidv4();
    const response = await axios({
      method: "get",
      url: `https://api.twitch.tv/helix/streams/followed?user_id=${userId}`,
      headers: {
        Authorization: `Bearer ${accessToken}`,
        "Client-Id": TWITCH_CLIENT_ID,
      },
    });

    for (const stream of response.data.data) {
      const data = {
        ...stream,
        fetchedAt: new Date(),
        fetchId,
      };

      await collection.findOneAndUpdate(
        { id: stream.id },
        { $set: data },
        { upsert: true, returnDocument: "after" }
      );
    }
    await fetchLogCollection.insertOne({ userId: userId, fetchedAt: new Date(), fetchId: fetchId });

    res.json(response.data);
  } catch (error) {
    console.error("Error fetching followed streams:", error);
    res.status(500).send("Error fetching followed streams");
  }
};
