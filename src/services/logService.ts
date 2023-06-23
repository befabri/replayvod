import { getDbInstance } from "../models/db";
import { Log } from "../models/logModel";

class LogService {
  async getLog(id: number) {
    const db = await getDbInstance();
    const logCollection = db.collection("logs");
    return logCollection.findOne({ id: id });
  }

  async getAllLogs() {
    const db = await getDbInstance();
    const logCollection = db.collection("logs");
    return logCollection.find().toArray();
  }
}

export default LogService;
