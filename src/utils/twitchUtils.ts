import axios from "axios";
import { getDbInstance } from "../models/db";

const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;
const TWITCH_SECRET = process.env.TWITCH_SECRET;

export const getAppAccessToken = async () => {
  const db = await getDbInstance();
  const collection = db.collection("appAccessToken");

  const currentTimestamp = Date.now();
  const tokenDocument = await collection.findOne({ expiresAt: { $gt: currentTimestamp } });

  if (tokenDocument) {
    return tokenDocument.accessToken;
  }

  try {
    const response = await axios.post("https://id.twitch.tv/oauth2/token", null, {
      params: {
        client_id: TWITCH_CLIENT_ID,
        client_secret: TWITCH_SECRET,
        grant_type: "client_credentials",
      },
    });

    const newToken = response.data.access_token;
    const tokenLifetime = response.data.expires_in * 1000; // Convert to milliseconds

    const newTokenDocument = {
      accessToken: newToken,
      expiresAt: currentTimestamp + tokenLifetime,
    };
    await collection.insertOne(newTokenDocument);

    return newToken;
  } catch (error) {
    console.error("Error getting app access token:", error);
    throw error;
  }
};
