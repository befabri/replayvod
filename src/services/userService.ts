// services/userService.ts
import axios from "axios";
import User from "../models/userModel";
import { connect, getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import { v4 as uuidv4 } from "uuid";
import { FollowedChannel, FollowedStream } from "../models/userModel";

class UserService {
  twitchAPI: TwitchAPI;

  constructor() {
    this.twitchAPI = new TwitchAPI();
  }

  async getUserFollowedStreams(userId: string, accessToken: string) {
    try {
      const db = await getDbInstance();
      const collection = db.collection("followedStreams");
      const fetchLogCollection = db.collection("fetchLog");
      const fetchLog = await fetchLogCollection.findOne({ userId: userId }, { sort: { fetchedAt: -1 } });
      if (fetchLog && fetchLog.fetchedAt > new Date(Date.now() - 5 * 60 * 1000)) {
        const streams = await collection.find({ fetchId: fetchLog.fetchId }).toArray();
        return streams;
      }
      const fetchId = uuidv4();
      const followedStreams = await this.twitchAPI.getAllFollowedStreams(userId, accessToken);
      const streamUserIds = followedStreams.map((stream: FollowedStream) => stream.user_id);
      const users = await this.twitchAPI.getUsers(streamUserIds);
      await this.storeUserDetails(users);
      for (const stream of followedStreams) {
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
      return { data: followedStreams };
    } catch (error) {
      console.error("Error fetching followed streams:", error);
      throw new Error("Error fetching followed streams");
    }
  }

  async getUserFollowedChannels(userId: string, accessToken: string) {
    try {
      const db = await getDbInstance();
      const followedChannelsCollection = db.collection("followedChannels");
      const oneDayAgo = new Date(Date.now() - 24 * 60 * 60 * 1000);
      const existingData = await followedChannelsCollection.findOne({ userId, fetchedAt: { $gte: oneDayAgo } });
      if (existingData) {
        return existingData.channels;
      }
      const followedChannels = await this.twitchAPI.getAllFollowedChannels(userId, accessToken);
      const channelsUserIds = followedChannels.map((channel: FollowedChannel) => channel.broadcaster_id);
      const users = await this.twitchAPI.getUsers(channelsUserIds);
      await this.storeUserDetails(users);
      const profilePictures = await this.getUserProfilePicture(channelsUserIds);
      const followedChannelsWithProfilePictures = followedChannels.map((channel) => ({
        ...channel,
        profile_picture: profilePictures[channel.broadcaster_id],
      }));

      await followedChannelsCollection.updateOne(
        { userId },
        {
          $set: {
            channels: followedChannelsWithProfilePictures,
            fetchedAt: new Date(),
            userId,
          },
        },
        { upsert: true }
      );
      return followedChannelsWithProfilePictures;
    } catch (error) {
      console.error("Error fetching followed channels from Twitch API:", error);
      throw new Error("Error fetching followed channels from Twitch API");
    }
  }

  async getUserDetail(userId: string) {
    const user = await this.twitchAPI.getUser(userId);
    return user;
  }

  async getUserDetailDB(userId: string) {
    const db = await getDbInstance();
    const userCollection = db.collection("users");
    const user = await userCollection.findOne({ id: userId });
    return user;
  }

  async getMultipleUserDetailsDB(userIds: string[]) {
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
    return users;
  }

  async updateUserDetail(userId: string) {
    const user = await this.twitchAPI.getUser(userId);
    if (user) {
      const db = await getDbInstance();
      const userCollection = db.collection("users");
      await userCollection.updateOne({ id: userId }, { $set: user }, { upsert: true });
    }
    return user;
  }

  async fetchAndStoreUserDetails(userIds: string[]) {
    const users = await this.twitchAPI.getUsers(userIds);
    await this.storeUserDetails(users);
    return "Users fetched and stored successfully.";
  }

  async storeUserDetails(users: any[]) {
    const db = await getDbInstance();
    const userCollection = db.collection("users");
    const bulkOps = users.map((user) => ({
      updateOne: {
        filter: { id: user.id },
        update: { $set: user },
        upsert: true,
      },
    }));
    await userCollection.bulkWrite(bulkOps);
  }

  getUserProfilePicture = async (userIds: string[]) => {
    const db = await getDbInstance();
    const userCollection = db.collection<User>("users");
    const users = await userCollection.find({ id: { $in: userIds } }).toArray();
    const profilePictures: Record<string, string> = {};
    users.forEach((user) => {
      profilePictures[user.id] = user.profile_image_url;
    });
    return profilePictures;
  };
}

export default UserService;
