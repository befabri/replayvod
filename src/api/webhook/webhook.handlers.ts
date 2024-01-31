import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import { Webhook } from "../../models/webhookModel";
import {
    MESSAGE_TYPE,
    MESSAGE_TYPE_NOTIFICATION,
    MESSAGE_TYPE_REVOCATION,
    MESSAGE_TYPE_VERIFICATION,
    TwitchHeaders,
} from "../../constants/twitchConstants";
import { NotificationBody, SubscriptionType } from "../../models/notificationTwitch";
import { webhookFeature } from ".";
import { logger as rootLogger } from "../../app";
const logger = rootLogger.child({ domain: "webhookHandler", service: "webhook" });

interface WebhookRequest extends RouteGenericInterface {
    Body: Webhook;
    Headers: TwitchHeaders;
}

export const callbackWebhook = async (req: FastifyRequest<WebhookRequest>, reply: FastifyReply) => {
    let notification: NotificationBody = req.body;
    let messageType = req.headers[MESSAGE_TYPE];
    let response;
    if (MESSAGE_TYPE_NOTIFICATION === messageType) {
        switch (notification.subscription.type) {
            case SubscriptionType.CHANNEL_UPDATE:
                response = await webhookFeature.handleChannelUpdate(notification);
                break;
            case SubscriptionType.STREAM_ONLINE:
                response = await webhookFeature.handleStreamOnline(notification);
                break;
            case SubscriptionType.STREAM_OFFLINE:
                response = await webhookFeature.handleStreamOffline(notification);
                break;
            default:
                response = await webhookFeature.handleNotification(notification);
                break;
        }
    } else if (MESSAGE_TYPE_VERIFICATION === messageType) {
        response = await webhookFeature.handleVerification(notification);
    } else if (MESSAGE_TYPE_REVOCATION === messageType) {
        response = await webhookFeature.handleRevocation(notification);
    } else {
        return reply.status(400).send();
    }
    reply.status(response.status);
    if (response.body) {
        return reply.send(response.body);
    }
    reply.send();
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
