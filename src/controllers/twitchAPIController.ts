import { Request, Response, NextFunction } from "express";
import TwitchAPI from "../utils/twitchAPI";
import { eventSubService, gameService } from "../services";

const twitchAPI = new TwitchAPI();

export const fetchAndSaveGames = async (req: Request, res: Response) => {
    try {
        const games = await twitchAPI.getAllGames();
        await gameService.saveGamesToDb(games);
        res.json({ message: "Games fetched and saved successfully." });
    } catch (err) {
        console.error(err);
        res.status(500).json({ error: "An error occurred while fetching and saving games." });
    }
};

export const getListEventSub = async (req: Request, res: Response) => {
    const userId = req.session?.passport?.user?.data[0]?.id;
    if (!userId || userId == undefined) {
        res.status(500).send("Error no user authenticated");
        return;
    }
    try {
        const eventSub = await eventSubService.getEventSub(userId);
        res.json({ data: eventSub.data, message: eventSub.message });
    } catch (err) {
        console.error(err);
        res.status(500).json({ error: "An error occurred while fetching EventSub subscriptions." });
    }
};

export const getTotalCost = async (req: Request, res: Response) => {
    try {
        const eventSub = await eventSubService.getTotalCost();
        res.json({ data: eventSub.data, message: eventSub.message });
    } catch (err) {
        console.error(err);
        res.status(500).json({ error: "An error occurred while fetching total cost." });
    }
};
