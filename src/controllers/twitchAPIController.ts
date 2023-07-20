import { Request, Response, NextFunction } from "express";
import GameService from "../services/gameService";
import TwitchAPI from "../utils/twitchAPI";

const twitchAPI = new TwitchAPI();

export const fetchAndSaveGames = async (req: Request, res: Response) => {
  try {
    const games = await twitchAPI.getAllGames();
    await GameService.saveGamesToDb(games);
    res.json({ message: "Games fetched and saved successfully." });
  } catch (err) {
    console.error(err);
    res.status(500).json({ error: "An error occurred while fetching and saving games." });
  }
};
