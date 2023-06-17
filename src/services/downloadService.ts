import { getDbInstance } from "../models/db";
import TwitchAPI from "../utils/twitchAPI";
import { v4 as uuidv4 } from "uuid";
import UserService from "./userService";

const userService = new UserService();

class downloadService {
  twitchAPI: TwitchAPI;

  constructor() {
    this.twitchAPI = new TwitchAPI();
  }

  async planningRecord(userId: string) {
    const user = await userService.getUserDetailDB(userId);
    console.log(user);
    return "Successful registration planning";
  }
}

export default downloadService;
