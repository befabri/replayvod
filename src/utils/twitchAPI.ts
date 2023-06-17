import axios from "axios";
import { getAppAccessToken } from "../utils/twitchUtils";
import { FollowedChannel, FollowedStream } from "../models/userModel";
import { chunkArray } from "../utils/utils";
import dotenv from "dotenv";
dotenv.config();
const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;

class TwitchAPI {
  async getUser(userId: string) {
    try {
      const response = await axios.get(`https://api.twitch.tv/helix/users?id=${userId}`, {
        headers: {
          Authorization: "Bearer " + process.env.ACCESS_TOKEN,
          "Client-ID": TWITCH_CLIENT_ID,
        },
      });

      return response.data.data[0];
    } catch (error) {
      console.error("Error fetching user details from Twitch API:", error);
      throw new Error("Failed to fetch user details from Twitch API");
    }
  }

  async getUsers(userIds: string[]) {
    try {
      const accessToken = await getAppAccessToken();
      const userIdChunks = chunkArray(userIds, 100);
      const responses = await Promise.all(
        userIdChunks.map(async (chunk) => {
          const params = chunk.map((id) => `id=${id}`).join("&");
          return axios.get(`https://api.twitch.tv/helix/users?${params}`, {
            headers: {
              Authorization: "Bearer " + accessToken,
              "Client-ID": TWITCH_CLIENT_ID,
            },
          });
        })
      );
      return responses.flatMap((response) => response.data.data);
    } catch (error) {
      console.error("Error fetching users details from Twitch API:", error);
      throw new Error("Failed to fetch users details from Twitch API");
    }
  }

  async getAllFollowedChannels(userId: string, accessToken: string, cursor?: string): Promise<FollowedChannel[]> {
    const response = await axios({
      method: "get",
      url: `https://api.twitch.tv/helix/channels/followed`,
      params: {
        user_id: userId,
        after: cursor,
        first: 100,
      },
      headers: {
        Authorization: `Bearer ${accessToken}`,
        "Client-Id": TWITCH_CLIENT_ID,
      },
    });

    const { data, pagination } = response.data;
    if (pagination && pagination.cursor) {
      const nextPageData = await this.getAllFollowedChannels(userId, accessToken, pagination.cursor);
      return data.concat(nextPageData);
    } else {
      return data;
    }
  }

  async getAllFollowedStreams(userId: string, accessToken: string, cursor?: string): Promise<FollowedStream[]> {
    const response = await axios({
      method: "get",
      url: `https://api.twitch.tv/helix/streams/followed`,
      params: {
        user_id: userId,
        after: cursor,
        first: 100,
      },
      headers: {
        Authorization: `Bearer ${accessToken}`,
        "Client-Id": TWITCH_CLIENT_ID,
      },
    });

    const { data, pagination } = response.data;
    if (pagination && pagination.cursor) {
      const nextPageData = await this.getAllFollowedStreams(userId, accessToken, pagination.cursor);
      return data.concat(nextPageData);
    } else {
      return data;
    }
  }
}
export default TwitchAPI;
