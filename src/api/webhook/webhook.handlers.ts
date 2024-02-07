import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import { Webhook } from "../../models/webhookModel";
import {
    MESSAGE_TYPE,
    MessageType,
    SubscriptionType,
    TwitchHeaders,
    TwitchNotificationBody,
    TwitchNotificationChallenge,
    TwitchNotificationEvent,
    TwitchNotificationRevocation,
} from "../../models/twitch";
import { webhookFeature } from ".";
import { channelFeature } from "../channel";
import {
    isTwitchNotificationChallenge,
    isTwitchNotificationEvent,
    isTwitchNotificationRevocation,
} from "./webhook";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "webhook", service: "webhookHandler" });

interface WebhookRequest extends RouteGenericInterface {
    Body: Webhook;
    Headers: TwitchHeaders;
}

export const callbackWebhook = async (req: FastifyRequest<WebhookRequest>, reply: FastifyReply) => {
    let notification: TwitchNotificationBody = req.body;
    let messageType = req.headers[MESSAGE_TYPE];
    let response;
    logger.info(`Received Twitch webhook with message type: ${messageType}`);
    if (messageType === MessageType.MESSAGE_TYPE_NOTIFICATION && isTwitchNotificationEvent(notification)) {
        logger.info(
            `Processing notification for subscription type: ${notification.subscription.type}, Subscription ID: ${notification.subscription.id}`
        );
        switch (notification.subscription.type) {
            case SubscriptionType.CHANNEL_UPDATE:
                response = await handleChannelUpdate(notification);
                break;
            case SubscriptionType.STREAM_ONLINE:
                response = await handleStreamOnline(notification);
                break;
            case SubscriptionType.STREAM_OFFLINE:
                response = await handleStreamOffline(notification);
                break;
            default:
                response = await handleNotification(notification);
                break;
        }
    } else if (
        messageType === MessageType.MESSAGE_TYPE_VERIFICATION &&
        isTwitchNotificationChallenge(notification)
    ) {
        logger.info(`Processing verification challenge for Subscription ID: ${notification.subscription.id}`);
        response = await handleVerification(notification);
    } else if (
        messageType === MessageType.MESSAGE_TYPE_REVOCATION &&
        isTwitchNotificationRevocation(notification)
    ) {
        logger.info(`Processing revocation for Subscription ID: ${notification.subscription.id}`);
        response = await handleRevocation(notification);
    } else {
        logger.info(`Processing revocation for Subscription ID: ${notification.subscription.id}`);
        return reply.status(400).send();
    }
    reply.status(response.status);
    if (response.body) {
        return reply.send(response.body);
    }
    reply.send();
};

export const handleVerification = async (
    notification: TwitchNotificationChallenge
): Promise<{ status: number; body: string }> => {
    return {
        status: 200,
        body: notification.challenge,
    };
};

export const handleChannelUpdate = async (
    _notification: TwitchNotificationBody
): Promise<{ status: number; body: null }> => {
    return {
        status: 204,
        body: null,
    };
};


// WIP
export const handleStreamOnline = async (
    notification: TwitchNotificationEvent
): Promise<{ status: number; body: null }> => {
    await webhookFeature.handleWebhookEvent(notification.subscription.type, notification.event);
    const fetchStream = async () => {
        try {
            const streamfetched = await channelFeature.getChannelStream(notification.event.broadcaster_user_id, "system");
            if (!streamfetched) {
                logger.error(`OFFLINE? Stream fetched error in handleStreamOnline for ${notification.event.broadcaster_user_id}`);
                setTimeout(fetchStream, 300000); // 300000 milliseconds = 5 minutes
            } else {
                logger.info(`Stream successfully fetched in handleStreamOnline for ${notification.event.broadcaster_user_id}`);
            }
        } catch (error) {
            logger.error(`Stream fetched error in handleStreamOnline for ${notification.event.broadcaster_user_id}`);
        }
    };
    await fetchStream();
    return {
        status: 204,
        body: null,
    };
};


export const handleStreamOffline = async (
    notification: TwitchNotificationEvent
): Promise<{ status: number; body: null }> => {
    await webhookFeature.handleWebhookEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleNotification = async (
    _notification: TwitchNotificationEvent
): Promise<{ status: number; body: null }> => {
    return {
        status: 204,
        body: null,
    };
};

export const handleRevocation = async (
    notification: TwitchNotificationRevocation
): Promise<{ status: number; body: null }> => {
    webhookFeature.handleRevocation(notification);
    return {
        status: 204,
        body: null,
    };
};

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
