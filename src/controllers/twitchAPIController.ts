import { Request, Response, NextFunction } from "express";
import GameService from "../services/gameService";
import TwitchAPI from "../utils/twitchAPI";
import eventSubService from "../services/eventSubService";

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

export const getSubscriptions = async (req: Request, res: Response) => {
    try {
        const sub = await twitchAPI.getEventSub();
        // await eventSubService.saveSubscriptionsToDb(sub);
        res.json(sub);
    } catch (err) {
        console.error(err);
        res.status(500).json({ error: "An error occurred while fetching and saving games." });
    }
};
