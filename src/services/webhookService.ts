import { Webhook } from "../models/webhookModel";
import { eventProcessingService } from "../services";
import { createHmac, timingSafeEqual } from "crypto";
import { TWITCH_MESSAGE_ID, TWITCH_MESSAGE_TIMESTAMP } from "../constants/twitchConstants";
import { logger as rootLogger } from "../app";
import { prisma } from "../server";
const logger = rootLogger.child({ service: "webhookService" });

const CALLBACK_URL_WEBHOOK = process.env.CALLBACK_URL_WEBHOOK;

export const addWebhook = async (webhook: Webhook) => {
    console.log(webhook);
    console.log(JSON.stringify(webhook));
};

export const removeWebhook = async (id: string) => {
    // const db = await getDbInstance();
    // const webhookCollection = db.collection("webhooks");
    // const webhook = await getWebhook(id);
    // if (webhook) {
    //     await webhookCollection.deleteOne({ _id: new ObjectId(webhook._id) });
    // }
    // return webhook;
    return id;
};

export const getWebhook = async (id: string) => {
    // return prisma.event.findUnique({
    //     where: { id: id },
    // });
    return id;
};

export const getAllWebhooks = async () => {
    return prisma.event.findMany();
};

export const getSecret = () => {
    return process.env.SECRET!;
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
    eventProcessingService.logEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleStreamOnline = (notification: any): { status: number; body: null } => {
    eventProcessingService.logEvent(notification.subscription.type, notification.event);
    eventProcessingService.handleDownload(notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleStreamOffline = (notification: any): { status: number; body: null } => {
    eventProcessingService.logEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleNotification = (notification: any): { status: number; body: null } => {
    eventProcessingService.logEvent(notification.subscription.type, notification.event);
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
