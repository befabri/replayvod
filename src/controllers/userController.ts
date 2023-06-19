import { Request, Response } from "express";
import UserService from "../services/userService";

const userService = new UserService();
const userCacheNotFound = new Map();
const userCache = new Map();

export const getUserFollowedStreams = async (req: Request, res: Response) => {
  if (!req.session?.passport?.user) {
    res.status(401).send("Unauthorized");
    return;
  }
  const userId = req.session?.passport?.user?.data[0]?.id;
  const accessToken = req.session?.passport?.user?.accessToken;
  if (!userId || !accessToken || userId == undefined) {
    res.status(500).send("Error fetching followed streams");
    return;
  }
  try {
    const followedStreams = await userService.getUserFollowedStreams(userId, accessToken);
    res.json(followedStreams);
  } catch (error) {
    console.error("Error fetching followed streams:", error);
    res.status(500).send("Error fetching followed streams");
  }
};

export const getUserFollowedChannels = async (req: Request, res: Response) => {
  if (!req.session?.passport?.user) {
    res.status(401).send("Unauthorized");
    return;
  }
  const userId = req.session?.passport?.user?.data[0]?.id;
  const accessToken = req.session?.passport?.user?.accessToken;
  if (!userId || !accessToken || userId == undefined) {
    res.status(500).send("Error fetching followed streams");
    return;
  }
  try {
    const followedChannels = await userService.getUserFollowedChannels(userId, accessToken);
    res.json(followedChannels);
  } catch (error) {
    console.error("Error fetching followed channels:", error);
    res.status(500).send("Error fetching followed channels");
  }
};

export const getUserDetail = async (req: Request, res: Response) => {
  const userId = req.params.id;

  if (!userId || typeof userId !== "string") {
    res.status(400).send("Invalid user id");
    return;
  }
  try {
    const user = await userService.getUserDetailDB(userId);
    if (!user) {
      res.status(404).send("User not found");
      return;
    }
    res.json(user);
  } catch (error) {
    console.error("Error fetching user details:", error);
    res.status(500).send("Error fetching user details");
  }
};

export const getUserDetailByName = async (req: Request, res: Response) => {
  const username = req.params.name;
  if (!username || typeof username !== "string") {
    res.status(400).send("Invalid user id");
    return;
  }
  try {
    if (userCacheNotFound.has(username)) {
      res.status(404).send("User not found");
      return;
    }
    if (userCache.has(username)) {
      res.json(userCache.get(username));
      return;
    }
    const user = await userService.getUserDetailByName(username);
    if (!user) {
      userCacheNotFound.set(username, true);
      res.status(404).send("User not found");
      return;
    }
    userCache.set(username, user);
    res.json(user);
  } catch (error) {
    console.error("Error fetching user details:", error);
    res.status(500).send("Error fetching user details");
  }
};

export const getMultipleUserDetailsFromDB = async (req: Request, res: Response) => {
  const queryUserIds = req.query.userIds;

  if (!queryUserIds) {
    res.status(400).send("Invalid 'userIds' field");
    return;
  }
  let userIds: string[];
  if (typeof queryUserIds === "string") {
    userIds = [queryUserIds];
  } else if (Array.isArray(queryUserIds) && typeof queryUserIds[0] === "string") {
    userIds = queryUserIds as string[];
  } else {
    res.status(400).send("Invalid 'userIds' field");
    return;
  }
  try {
    const users = await userService.getMultipleUserDetailsDB(userIds);
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
    const user = await userService.updateUserDetail(userId);
    res.json(user);
  } catch (error) {
    console.error("Error updating user details:", error);
    res.status(500).send("Error updating user details");
  }
};

export const fetchAndStoreUserDetails = async (req: Request, res: Response) => {
  const userIds = req.body.userIds;
  if (!Array.isArray(userIds) || !userIds.every((id) => typeof id === "string")) {
    res.status(400).send("Invalid 'userIds' field");
    return;
  }
  try {
    const message = await userService.fetchAndStoreUserDetails(userIds);
    res.status(200).send(message);
  } catch (error) {
    console.error("Error fetching and storing user details:", error);
    res.status(500).send("Error fetching and storing user details");
  }
};

export const updateUsers = async (req: Request, res: Response) => {
  if (!req.session?.passport?.user) {
    res.status(401).send("Unauthorized");
    return;
  }
  const userId = req.session?.passport?.user?.data[0]?.id;
  const accessToken = req.session?.passport?.user?.accessToken;
  if (!userId || !accessToken || userId == undefined) {
    res.status(500).send("Error fetching followed streams");
    return;
  }
  try {
    const result = await userService.updateUsers(userId);
    res.status(200).send(result);
  } catch (error) {
    console.error("Error updating users:", error);
    res.status(500).send("Error updating users");
  }
};
