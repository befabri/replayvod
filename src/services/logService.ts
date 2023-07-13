import { getDbInstance } from "../models/db";
import { Log } from "../models/logModel";

export const getLog = async (id: number) => {
    const db = await getDbInstance();
    const logCollection = db.collection("logs");
    return logCollection.findOne({ id: id });
};

export const getAllLogs = async () => {
    const db = await getDbInstance();
    const logCollection = db.collection("logs");
    return logCollection.find().toArray();
};

export default {
    getLog,
    getAllLogs,
};
