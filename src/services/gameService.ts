import { MongoClient, Collection } from "mongodb";
import { getDbInstance } from "../models/db";
import { Game } from "../models/twitchModel";

class GameService {
  private gameCollection!: Collection<Game>;

  constructor() {
    this.getDbCollection();
  }

  private async getDbCollection() {
    const db = await getDbInstance();
    this.gameCollection = db.collection("games");
  }

  async saveGamesToDb(games: Game[]) {
    for (const game of games) {
      await this.gameCollection.updateOne({ id: game.id }, { $set: game }, { upsert: true });
    }
  }

  async getAllGames() {
    return await this.gameCollection.find().toArray();
  }

  async getGameById(id: string) {
    return await this.gameCollection.findOne({ id: id });
  }

  async getGameByName(name: string) {
    return await this.gameCollection.findOne({ name: name });
  }
}

export default new GameService();
