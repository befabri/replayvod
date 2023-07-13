import { Document, ObjectId } from "mongodb";
import { getDbInstance } from "../models/db";
import { Webhook } from "../models/webhookModel";
import { eventProcessingService } from "../services";
import { createHmac, timingSafeEqual } from "crypto";
import { TWITCH_MESSAGE_ID, TWITCH_MESSAGE_TIMESTAMP } from "../constants/twitchConstants";
import { webhookEventLogger } from "../middlewares/loggerMiddleware";

const CALLBACK_URL_WEBHOOK = process.env.CALLBACK_URL_WEBHOOK;

export const addWebhook = async (webhook: Webhook) => {
    const db = await getDbInstance();
    const webhookCollection = db.collection("webhooks");
    await webhookCollection.insertOne(webhook);
    return webhook;
};

export const removeWebhook = async (id: string) => {
    const db = await getDbInstance();
    const webhookCollection = db.collection("webhooks");
    const webhook = await getWebhook(id);
    if (webhook) {
        await webhookCollection.deleteOne({ _id: new ObjectId(webhook._id) });
    }
    return webhook;
};

export const getWebhook = async (id: string) => {
    const db = await getDbInstance();
    const webhookCollection = db.collection("webhooks");
    return webhookCollection.findOne({ id: id });
};

export const getAllWebhooks = async () => {
    const db = await getDbInstance();
    const webhookCollection = db.collection("webhooks");
    return webhookCollection.find().toArray();
};

export const getSecret = () => {
    return process.env.SECRET;
};

export const getHmacMessage = (request) => {
    return (
        request.headers[TWITCH_MESSAGE_ID] +
        request.headers[TWITCH_MESSAGE_TIMESTAMP] +
        JSON.stringify(request.body)
    );
};

export const getHmac = (secret: string, message: string): string => {
    return createHmac("sha256", secret).update(message).digest("hex");
};

export const getCallbackUrlWebhook = (): string => {
    return CALLBACK_URL_WEBHOOK;
};

export const verifyMessage = (hmac: string, verifySignature: string): boolean => {
    return timingSafeEqual(Buffer.from(hmac), Buffer.from(verifySignature));
};

export const handleChannelUpdate = (notification: any): { status: number; body: null } => {
    webhookEventLogger.info("Channel updated");
    webhookEventLogger.info(JSON.stringify(notification.event, null, 4));
    eventProcessingService.logEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleStreamOnline = (notification: any): { status: number; body: null } => {
    webhookEventLogger.info("Stream went online");
    webhookEventLogger.info(JSON.stringify(notification.event, null, 4));
    eventProcessingService.logEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleStreamOffline = (notification: any): { status: number; body: null } => {
    webhookEventLogger.info("Stream went offline");
    webhookEventLogger.info(JSON.stringify(notification.event, null, 4));
    eventProcessingService.logEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleNotification = (notification: any): { status: number; body: null } => {
    eventProcessingService.logEvent(notification.subscription.type, notification.event);
    webhookEventLogger.info(`Event type: ${notification.subscription.type}`);
    webhookEventLogger.info(JSON.stringify(notification.event, null, 4));
    return {
        status: 204,
        body: null,
    };
};

export const handleVerification = (notification: any): { status: number; body: string } => {
    return {
        status: 200,
        body: notification.challenge,
    };
};

export const handleRevocation = (notification: any): { status: number; body: null } => {
    eventProcessingService.handleRevocation(notification);
    return {
        status: 204,
        body: null,
    };
};

export default {
    addWebhook,
    removeWebhook,
    getWebhook,
    getAllWebhooks,
    getSecret,
    getHmacMessage,
    getHmac,
    getCallbackUrlWebhook,
    verifyMessage,
    handleChannelUpdate,
    handleStreamOnline,
    handleStreamOffline,
    handleNotification,
    handleVerification,
    handleRevocation,
};
