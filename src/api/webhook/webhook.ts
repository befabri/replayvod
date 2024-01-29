import { env } from "../../app";
import { prisma } from "../../server";
import { eventSubProcessingFeature } from ".";

export const getWebhook = async (id: string) => {
    // return prisma.event.findUnique({
    //     where: { id: id },
    // });
    return id;
};

export const getAllWebhooks = async () => {
    return prisma.webhookEvent.findMany();
};

export const getSecret = () => {
    return env.secret;
};

export const getCallbackUrlWebhook = (): string => {
    return env.callbackUrlWebhook;
};

export const handleChannelUpdate = async (notification: any): Promise<{ status: number; body: null }> => {
    eventSubProcessingFeature.logEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleStreamOnline = async (notification: any): Promise<{ status: number; body: null }> => {
    eventSubProcessingFeature.logEvent(notification.subscription.type, notification.event);
    eventSubProcessingFeature.handleWebhookEvent(notification.subscription.type, notification.event);
    eventSubProcessingFeature.handleDownload(notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleStreamOffline = async (notification: any): Promise<{ status: number; body: null }> => {
    eventSubProcessingFeature.logEvent(notification.subscription.type, notification.event);
    eventSubProcessingFeature.handleWebhookEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleNotification = async (notification: any): Promise<{ status: number; body: null }> => {
    eventSubProcessingFeature.logEvent(notification.subscription.type, notification.event);
    return {
        status: 204,
        body: null,
    };
};

export const handleVerification = async (notification: any): Promise<{ status: number; body: string }> => {
    return {
        status: 200,
        body: notification.challenge,
    };
};

export const handleRevocation = async (notification: any): Promise<{ status: number; body: null }> => {
    eventSubProcessingFeature.handleRevocation(notification);
    return {
        status: 204,
        body: null,
    };
};
