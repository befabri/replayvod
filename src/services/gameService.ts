import { getDbInstance } from "../models/db";
import { Game } from "../models/twitchModel";

export const saveGamesToDb = async (games: Game[]) => {
    const db = await getDbInstance();
    const gameCollection = db.collection("games");
    for (const game of games) {
        await gameCollection.updateOne({ id: game.id }, { $set: game }, { upsert: true });
    }
};

export const getAllGames = async () => {
    const db = await getDbInstance();
    const gameCollection = db.collection("games");
    return await gameCollection.find().toArray();
};

export const getGameById = async (id: string) => {
    const db = await getDbInstance();
    const gameCollection = db.collection("games");
    return await gameCollection.findOne({ id: id });
};

export const getGameByName = async (name: string) => {
    const db = await getDbInstance();
    const gameCollection = db.collection("games");
    return await gameCollection.findOne({ name: name });
};

export default {
    saveGamesToDb,
    getAllGames,
    getGameById,
    getGameByName,
};
