import axios from "axios";
import { Request, Response } from "express";
import { connect, getDbInstance } from "../models/db";
import { getAppAccessToken } from "../utils/twitchUtils";

const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;

import { v4 as uuidv4 } from "uuid";

interface User {
  id: string;
  login: string;
  display_name: string;
  type: string;
  broadcaster_type: string;
  description: string;
  profile_image_url: string;
  offline_image_url: string;
  view_count: number;
  email: string;
  created_at: string;
}

export const followList = async (req: Request, res: Response) => {
  if (!req.session?.passport?.user) {
    res.status(401).send("Unauthorized");
    return;
  }

  const userId = req.session.passport.user.data[0].id;
  const accessToken = req.session.passport.user.accessToken;

  if (userId == undefined) {
    res.status(500).send("Error fetching followed streams");
    return;
  }

  try {
    const db = await getDbInstance();
    const collection = db.collection("followedStreams");
    const fetchLogCollection = db.collection("fetchLog");
    const fetchLog = await fetchLogCollection.findOne({ userId: userId }, { sort: { fetchedAt: -1 } });
    if (fetchLog && fetchLog.fetchedAt > new Date(Date.now() - 5 * 60 * 1000)) {
      const streams = await collection.find({ fetchId: fetchLog.fetchId }).toArray();

      // Fetch profile picture data
      const streamUserIds = streams.map((stream: any) => stream.user_id);
      const profilePictures = await getUserProfilePicture(streamUserIds);

      // Add profile picture data to each stream
      const streamsWithProfilePictures = streams.map((stream: any) => ({
        ...stream,
        profilePicture: profilePictures[stream.user_id],
      }));

      res.json(streamsWithProfilePictures);
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

    const streamUserIds = response.data.data.map((stream: any) => stream.user_id);

    // Fetch user data from Twitch
    const usersData = await fetchUsersFromTwitch(streamUserIds);

    // Store user data
    await storeUserDetails(usersData);

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

    // Fetch profile picture data
    const profilePictures = await getUserProfilePicture(streamUserIds);

    // Add profile picture data to each stream
    const streamsWithProfilePictures = response.data.data.map((stream: any) => ({
      ...stream,
      profilePicture: profilePictures[stream.user_id],
    }));

    // Add profile picture data to the response
    res.json({ ...response.data, profilePictures: streamsWithProfilePictures });
  } catch (error) {
    console.error("Error fetching followed streams:", error);
    res.status(500).send("Error fetching followed streams");
  }
};

export const getUserDetail = async (req: Request, res: Response) => {
  const userId = req.params.id;

  if (!userId || typeof userId !== "string") {
    res.status(400).send("Invalid user id");
    return;
  }

  const db = await getDbInstance();
  const userCollection = db.collection("users");

  let user = await userCollection.findOne({ id: userId });

  if (!user) {
    res.status(404).send("User not found");
    return;
  }

  res.json(user);
};

export const getMultipleUserDetailsFromDB = async (req: Request, res: Response) => {
  let userIds = req.query.userIds;

  // if userIds is a string, make it an array
  if (typeof userIds === "string") {
    userIds = [userIds];
  }

  if (!Array.isArray(userIds)) {
    res.status(400).send("Invalid 'userIds' field");
    return;
  }

  try {
    const db = await getDbInstance();
    const userCollection = db.collection("users");

    const users = [];

    for (const id of userIds) {
      if (typeof id === "string") {
        const user = await userCollection.findOne({ id });
        if (user) {
          users.push(user);
        }
      }
    }

    res.json(users);
  } catch (error) {
    console.error("Error fetching user details from database:", error);
    res.status(500).send("Error fetching user details from database");
  }
};

export const updateUserDetail = async (req: Request, res: Response) => {
  const userId = req.params.id;

  if (!userId || typeof userId !== "string") {
    res.status(400).send("Invalid user id");
    return;
  }

  try {
    const accessToken = await getAppAccessToken();
    const response = await axios({
      method: "get",
      url: `https://api.twitch.tv/helix/users`,
      params: {
        id: userId,
      },
      headers: {
        Authorization: `Bearer ${accessToken}`,
        "Client-Id": TWITCH_CLIENT_ID,
      },
    });

    const user = response.data.data[0];

    const db = await getDbInstance();
    const userCollection = db.collection("users");

    await userCollection.updateOne({ id: userId }, { $set: user }, { upsert: true });

    res.json(user);
  } catch (error) {
    console.error("Error fetching user details:", error);
    res.status(500).send("Error fetching user details");
  }
};

export const fetchAndStoreUserDetails = async (req: Request, res: Response) => {
  const userIds = req.body.userIds;
  if (!Array.isArray(userIds) || !userIds.every((id) => typeof id === "string")) {
    res.status(400).send("Invalid 'userIds' field");
    return;
  }

  try {
    const usersData = await fetchUsersFromTwitch(userIds);
    await storeUserDetails(usersData);
    res.status(200).send("Users data fetched and stored successfully.");
  } catch (error) {
    console.error("Error fetching and storing user details:", error);
    res.status(500).send("Error fetching and storing user details");
  }
};

const getUserProfilePicture = async (userIds: string[]) => {
  // get DB instance and user collection
  const db = await getDbInstance();
  const userCollection = db.collection<User>("users");

  // find users in the collection
  const users = await userCollection.find({ id: { $in: userIds } }).toArray();

  // Create a mapping of userIds to their profile pictures
  const profilePictures: Record<string, string> = {};
  users.forEach((user) => {
    profilePictures[user.id] = user.profile_image_url;
  });
  return profilePictures;
};

const fetchUsersFromTwitch = async (userIds: string[]) => {
  const accessToken = await getAppAccessToken();
  const params = userIds.map((id) => `id=${id}`).join("&");
  const response = await axios({
    method: "get",
    url: `https://api.twitch.tv/helix/users?${params}`,
    headers: {
      Authorization: `Bearer ${accessToken}`,
      "Client-Id": TWITCH_CLIENT_ID,
    },
  });
  return response.data.data;
};

const storeUserDetails = async (usersData: any[]) => {
  const db = await getDbInstance();
  const userCollection = db.collection("users");

  const bulkOps = usersData.map((user) => ({
    updateOne: {
      filter: { id: user.id },
      update: { $set: user },
      upsert: true,
    },
  }));

  await userCollection.bulkWrite(bulkOps);
};
