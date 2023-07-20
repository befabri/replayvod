import { webhookEventLogger } from "../middlewares/loggerMiddleware";
import { downloadSchedule } from "../services/downloadService";

export const logEvent = (eventType: string, event: any) => {
    // Implementation for logging event
    webhookEventLogger.info(`Received event: ${eventType}`);
    webhookEventLogger.info(JSON.stringify(event, null, 2));
};

export const handleRevocation = (notification: any) => {
    // Implementation for handling revocation
    webhookEventLogger.info("Received a revocation:");
    webhookEventLogger.info(JSON.stringify(notification, null, 2));
    webhookEventLogger.info(`${notification.subscription.type} notifications revoked!`);
    webhookEventLogger.info(`Reason: ${notification.subscription.status}`);
    webhookEventLogger.info(`Condition: ${JSON.stringify(notification.subscription.condition, null, 4)}`);
    // TODO: Add any additional logic needed to handle revocation, such as
    // updating your application's internal state or notifying other services.
};

export const handleDownload = (event: any) => {
    console.log(event, event.broadcaster_user_id);
    webhookEventLogger.info(event, event.broadcaster_user_id);
    const broadcaster_id = event.broadcaster_user_id;
    downloadSchedule(broadcaster_id).catch((error) => {
        console.error("Error in downloadSchedule:", error);
        webhookEventLogger.error("Error in downloadSchedule:", error);
    });
};

export default {
    logEvent,
    handleRevocation,
    handleDownload,
};
