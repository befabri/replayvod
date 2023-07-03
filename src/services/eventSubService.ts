import { webhookEventLogger } from "../middlewares/loggerMiddleware";
import WebhookService from "./webhookService";
import UserService from "./userService";
import TwitchAPI from "../utils/twitchAPI";
import { EventSub } from "../models/webhookModel";
import { getDbInstance } from "../models/db";
import { v4 as uuidv4 } from "uuid";

class eventSubService {
    private userService = new UserService();
    private webhookService = new WebhookService();
    twitchAPI: TwitchAPI;

    constructor() {
        this.twitchAPI = new TwitchAPI();
    }

    async subToAllStreamEventFollowed() {
        console.log("getting followed channels");
        const followedChannels = await this.userService.getUserFollowedChannelsDb();
        console.log(JSON.stringify(followedChannels));
        let responses = [];
        for (const channel of followedChannels) {
            console.log(channel);
            console.log(channel.broadcaster.id);
            const respOnline = await this.subscribeToStreamOnline(channel.broadcaster.id);
            const respOffline = await this.subscribeToStreamOffline(channel.broadcaster.id);
            responses.push({ channel: channel.broadcaster.id, online: respOnline, offline: respOffline });
        }
        for (const resp of responses) {
            webhookEventLogger.info(
                `Channel ${resp.channel} - Online Response: ${resp.online}, Offline Response: ${resp.offline}`
            );
        }
    }

    async subscribeToStreamOnline(userId: string) {
        return await this.twitchAPI.createEventSub(
            "stream.online",
            "1",
            { broadcaster_user_id: userId },
            {
                method: "webhook",
                callback: this.webhookService.getCallbackUrlWebhook,
                secret: this.webhookService.getSecret(),
            }
        );
    }

    async subscribeToStreamOffline(userId: string) {
        return await this.twitchAPI.createEventSub(
            "stream.offline",
            "1",
            { broadcaster_user_id: userId },
            {
                method: "webhook",
                callback: this.webhookService.getCallbackUrlWebhook,
                secret: this.webhookService.getSecret(),
            }
        );
    }

    async getEventSub(userId: string) {
        const db = await getDbInstance();
        const collection = db.collection("eventSub");
        const fetchLogCollection = db.collection("fetchLog");
        const fetchLog = await fetchLogCollection.findOne(
            { userId: userId, type: "eventSub" },
            { sort: { fetchedAt: -1 } }
        );
        if (fetchLog && fetchLog.fetchedAt > new Date(Date.now() - 5 * 60 * 1000)) {
            const recentData = await collection.findOne({ userId: userId });
            if (!recentData) {
                return { data: [], message: "There is no EventSub subscription" };
            }
            return recentData;
        }
        const fetchId = uuidv4();
        const response = await this.twitchAPI.getEventSub();
        await fetchLogCollection.insertOne({
            userId: userId,
            fetchedAt: new Date(),
            fetchId: fetchId,
            type: "eventSub",
        });
        if (response && response.total === 0) {
            return { data: [], message: "There is no EventSub subscription" };
        }
        const data = await Promise.all(
            response.data.map(async (eventSub: any): Promise<EventSub> => {
                const user = await this.userService.getUserDetailDB(
                    eventSub.condition.broadcaster_user_id || eventSub.condition.user_id
                );
                return {
                    id: eventSub.id,
                    status: eventSub.status,
                    type: eventSub.type,
                    user_id: eventSub.condition.broadcaster_user_id || eventSub.condition.user_id,
                    user_login: user.login,
                    created_at: new Date(eventSub.created_at),
                    cost: eventSub.cost,
                };
            })
        );
        const result = await this.storeEventSub(data, userId, fetchId);
        return { data: data, message: result };
    }

    async storeEventSub(eventSubs: EventSub[], userId: string, fetchId: string) {
        const db = await getDbInstance();
        const eventSubCollection = db.collection("eventSub");
        await eventSubCollection.replaceOne(
            { userId: userId },
            { userId: userId, data: eventSubs, fetchedAt: new Date(), fetchId },
            { upsert: true }
        );
        return "EventSub subscriptions stored successfully.";
    }

    async getTotalCost() {
        const response = await this.twitchAPI.getEventSub();
        if (response && response.total === 0) {
            return { data: null, message: "There is no EventSub subscription" };
        }
        return {
            data: {
                total: response.total,
                total_cost: response.total_cost,
                max_total_cost: response.max_total_cost,
            },
            message: "Total cost retrieved successfully",
        };
    }
}
export default eventSubService;
