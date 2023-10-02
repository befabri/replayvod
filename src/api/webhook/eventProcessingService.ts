import { downloadSchedule } from "../download";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "webhook", service: "eventProcessingService" });

export const logEvent = (eventType: string, event: any) => {
    // Implementation for logging event
    logger.info(`Received event: ${eventType}`);
    logger.info(JSON.stringify(event, null, 2));
};

export const handleRevocation = (notification: any) => {
    // Implementation for handling revocation
    logger.info("Received a revocation:");
    logger.info(JSON.stringify(notification, null, 2));
    logger.info(`${notification.subscription.type} notifications revoked!`);
    logger.info(`Reason: ${notification.subscription.status}`);
    logger.info(`Condition: ${JSON.stringify(notification.subscription.condition, null, 4)}`);
    // TODO: Add any additional logic needed to handle revocation, such as
    // updating your application's internal state or notifying other services.
};

export const handleDownload = (event: any) => {
    logger.info(event, event.broadcaster_user_id);
    const broadcaster_id = event.broadcaster_user_id;
    downloadSchedule(broadcaster_id).catch((error) => {
        logger.error("Error in downloadSchedule:", error);
    });
};
