import { webhookEventLogger } from "../middlewares/loggerMiddleware";
import WebhookService from "./webhookService";
import UserService from "./userService";
import TwitchAPI from "../utils/twitchAPI";

class eventSubService {
    private userService = new UserService();
    private webhookService = new WebhookService();
    twitchAPI: TwitchAPI;

    constructor() {
        this.twitchAPI = new TwitchAPI();
    }
    async subToAllStreamEventFollowed() {
        const followedChannels = await this.userService.getUserFollowedChannelsDb();
        for (const channel of followedChannels) {
            const respOnline = await this.subscribeToStreamOnline(channel.broadcaster.id);
            const respOffline = await this.subscribeToStreamOffline(channel.broadcaster.id);
            webhookEventLogger.info(respOnline);
            webhookEventLogger.info(respOffline);
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
}

export default eventSubService;
