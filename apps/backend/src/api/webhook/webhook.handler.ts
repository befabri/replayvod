import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import {
    MESSAGE_TYPE,
    MessageType,
    SubscriptionType,
    TwitchHeaders,
    TwitchNotificationBody,
    TwitchNotificationRevocation,
} from "../../models/model.twitch";
import { logger as rootLogger } from "../../app";
import { WebhookService } from "./webhook.service";
const logger = rootLogger.child({ domain: "webhook", service: "handler" });

interface WebhookRequest extends RouteGenericInterface {
    Body: TwitchNotificationBody;
    Headers: TwitchHeaders;
}

export class WebhookHandler {
    callbackWebhook = async (req: FastifyRequest<WebhookRequest>, reply: FastifyReply) => {
        const service = req.server.webhook.service;

        let notification = req.body;
        let messageType = req.headers[MESSAGE_TYPE];
        logger.info(`Received Twitch webhook with message type: ${messageType}`);

        if (
            messageType === MessageType.MESSAGE_TYPE_VERIFICATION &&
            service.isTwitchNotificationChallenge(notification)
        ) {
            logger.info(`Processing verification challenge for Subscription ID: ${notification.subscription.id}`);
            return reply.status(200).send(notification.challenge);
        } else if (
            messageType === MessageType.MESSAGE_TYPE_NOTIFICATION &&
            service.isTwitchNotificationEvent(notification)
        ) {
            this.processNotificationAsync(service, notification);
            return reply.status(204).send();
        } else if (
            messageType === MessageType.MESSAGE_TYPE_REVOCATION &&
            service.isTwitchNotificationRevocation(notification)
        ) {
            this.processRevocationAsync(service, notification);
            return reply.status(204).send();
        } else {
            logger.info(
                `Unsupported message type or invalid notification for Subscription ID: ${notification.subscription.id}`
            );
            return reply.status(400).send();
        }
    };

    private async processNotificationAsync(service: WebhookService, notification: TwitchNotificationBody) {
        try {
            logger.info(`Processing notification with: ${notification.subscription.type}`);
            if (service.isTwitchNotificationEvent(notification)) {
                switch (notification.subscription.type) {
                    case SubscriptionType.CHANNEL_UPDATE:
                        await service.handleChannelUpdate(notification);
                        break;
                    case SubscriptionType.STREAM_ONLINE:
                        await service.handleStreamOnline(notification);
                        break;
                    case SubscriptionType.STREAM_OFFLINE:
                        await service.handleStreamOffline(notification);
                        break;
                    default:
                        await service.handleNotification(notification);
                        break;
                }
            }
        } catch (error) {
            logger.error(`Error processing notification: ${error.message}`);
        }
    }

    private async processRevocationAsync(service: WebhookService, notification: TwitchNotificationRevocation) {
        try {
            logger.info(
                `Asynchronously processing revocation for Subscription ID: ${notification.subscription.id}`
            );
            await service.handleRevocation(notification);
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
}
