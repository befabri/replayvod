import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import {
    MESSAGE_TYPE,
    MessageType,
    SubscriptionType,
    TwitchHeaders,
    TwitchNotificationBody,
    TwitchNotificationRevocation,
} from "../../models/twitch";
import { webhookFeature } from ".";
import {
    isTwitchNotificationChallenge,
    isTwitchNotificationEvent,
    isTwitchNotificationRevocation,
} from "./webhook";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "webhook", service: "webhookHandler" });

interface WebhookRequest extends RouteGenericInterface {
    Body: TwitchNotificationBody;
    Headers: TwitchHeaders;
}

export const callbackWebhook = async (req: FastifyRequest<WebhookRequest>, reply: FastifyReply) => {
    let notification = req.body;
    let messageType = req.headers[MESSAGE_TYPE];
    logger.info(`Received Twitch webhook with message type: ${messageType}`);

    if (messageType === MessageType.MESSAGE_TYPE_VERIFICATION && isTwitchNotificationChallenge(notification)) {
        logger.info(`Processing verification challenge for Subscription ID: ${notification.subscription.id}`);
        return reply.status(200).send(notification.challenge);
    } else if (messageType === MessageType.MESSAGE_TYPE_NOTIFICATION && isTwitchNotificationEvent(notification)) {
        processNotificationAsync(notification);
        return reply.status(204).send();
    } else if (
        messageType === MessageType.MESSAGE_TYPE_REVOCATION &&
        isTwitchNotificationRevocation(notification)
    ) {
        processRevocationAsync(notification);
        return reply.status(204).send();
    } else {
        logger.info(
            `Unsupported message type or invalid notification for Subscription ID: ${notification.subscription.id}`
        );
        return reply.status(400).send();
    }
};

async function processNotificationAsync(notification: TwitchNotificationBody) {
    try {
        let messageType = notification.message_type;
        logger.info(`Asynchronously processing notification with message type: ${messageType}`);
        if (isTwitchNotificationEvent(notification)) {
            switch (notification.subscription.type) {
                case SubscriptionType.CHANNEL_UPDATE:
                    await webhookFeature.handleChannelUpdate(notification);
                    break;
                case SubscriptionType.STREAM_ONLINE:
                    await webhookFeature.handleStreamOnline(notification);
                    break;
                case SubscriptionType.STREAM_OFFLINE:
                    await webhookFeature.handleStreamOffline(notification);
                    break;
                default:
                    await webhookFeature.handleNotification(notification);
                    break;
            }
        }
        logger.info("suc");
    } catch (error) {
        logger.error(`Error processing revocation: ${error.message}`);
    }
}

async function processRevocationAsync(notification: TwitchNotificationRevocation) {
    try {
        logger.info(`Asynchronously processing revocation for Subscription ID: ${notification.subscription.id}`);
        await webhookFeature.handleRevocation(notification);
    } catch (error) {
        logger.error(`Error processing revocation: ${error.message}`);
    }
}

// export const test = async (req: FastifyRequest<WebhookRequest>, reply: FastifyReply) => {
//     const notification = {
//         subscription: {
//             type: "stream.offline",
//         },
//         event: {
//             broadcaster_user_id: "24253306",
//             broadcaster_user_login: "packam",
//             broadcaster_user_name: "Packam",
//         },
//     };
//     const response = await webhookFeature.handleStreamOffline(notification);
//     console.log(response);
// };
