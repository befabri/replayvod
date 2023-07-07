import { MongoClient, Db } from "mongodb";
import dotenv from "dotenv";
dotenv.config();

const {
    MONGO_USERNAME: USERNAME,
    MONGO_PASSWORD: rawPASSWORD,
    MONGO_IP: DBIP,
    MONGO_IP_PROD: DBIP_PROD,
    MONGO_PORT: DBPORT,
    MONGO_DBNAME: DBNAME,
    MONGO_COLLECTION: COLLECTION = "defaultCollection",
    NODE_ENV: ENVIRONMENT = "development",
} = process.env;

const PASSWORD = encodeURIComponent(rawPASSWORD);

let url = `mongodb://${USERNAME}:${PASSWORD}@${DBIP}:${DBPORT}/${DBNAME}?retryWrites=true&w=majority&tls=true&authMechanism=DEFAULT&authSource=vod&tlsInsecure=true`;

if (ENVIRONMENT === "production") {
    // url = `mongodb+srv://${USERNAME}:${PASSWORD}@${DBIP_PROD}/${DBNAME}?tls=true&authSource=admin&retryWrites=true&w=majority`;
    // url = `mongodb://${USERNAME}:${PASSWORD}@${DBIP_PROD}:27017/${DBNAME}?authSource=admin&retryWrites=true&w=majority`;
    url = `mongodb://${USERNAME}:${PASSWORD}@${DBIP_PROD}:${DBPORT}/${DBNAME}?retryWrites=true&w=majority&tls=true&authMechanism=DEFAULT&authSource=vod&tlsInsecure=true`;
}

let client: MongoClient | null = null;
let connection: Promise<Db> | undefined;

async function connect(): Promise<Db> {
    if (!connection) {
        console.log("Establishing initial connection...");
        client = new MongoClient(url);
        await client.connect();
        connection = Promise.resolve(client.db(DBNAME));
    }
    if (client !== null) {
        return connection;
    }
    throw new Error("Failed to establish database connection.");
}

async function getDbInstance() {
    const db = await connect();
    return db;
}

async function cleanUp(): Promise<void> {
    if (client !== null) {
        await client.close();
    }
    process.exit(0);
}

process.on("SIGINT", cleanUp);
process.on("SIGTERM", cleanUp);

export { connect, getDbInstance };
