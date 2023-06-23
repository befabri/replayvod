import { Document, ObjectId } from "mongodb";
import { getDbInstance } from "../models/db";
import { Webhook } from "../models/webhookModel";
import EventProcessingService from "./eventProcessingService";
import { createHmac, timingSafeEqual } from "crypto";
import { TWITCH_MESSAGE_ID, TWITCH_MESSAGE_TIMESTAMP } from "../const/twitchConstants";

class WebhookService {
  private eventProcessingService: EventProcessingService;

  constructor() {
    this.eventProcessingService = new EventProcessingService();
  }

  async addWebhook(webhook: Webhook) {
    const db = await getDbInstance();
    const webhookCollection = db.collection("webhooks");
    await webhookCollection.insertOne(webhook);
    return webhook;
  }

  async removeWebhook(id: string) {
    const db = await getDbInstance();
    const webhookCollection = db.collection("webhooks");
    const webhook = await this.getWebhook(id);
    if (webhook) {
      await webhookCollection.deleteOne({ _id: new ObjectId(webhook._id) });
    }
    return webhook;
  }

  async getWebhook(id: string) {
    const db = await getDbInstance();
    const webhookCollection = db.collection("webhooks");
    return webhookCollection.findOne({ id: id });
  }

  async getAllWebhooks() {
    const db = await getDbInstance();
    const webhookCollection = db.collection("webhooks");
    return webhookCollection.find().toArray();
  }

  getSecret() {
    return process.env.TWITCH_SECRET;
  }

  getHmacMessage(request) {
    return (
      request.headers[TWITCH_MESSAGE_ID] + request.headers[TWITCH_MESSAGE_TIMESTAMP] + JSON.stringify(request.body)
    );
  }

  getHmac(secret: string, message: string): string {
    return createHmac("sha256", secret).update(message).digest("hex");
  }

  verifyMessage(hmac: string, verifySignature: string): boolean {
    return timingSafeEqual(Buffer.from(hmac), Buffer.from(verifySignature));
  }

  handleNotification(notification: any): { status: number; body: null } {
    this.eventProcessingService.logEvent(notification.subscription.type, notification.event);
    console.log(`Event type: ${notification.subscription.type}`);
    console.log(JSON.stringify(notification.event, null, 4));
    return {
      status: 204,
      body: null,
    };
  }

  handleVerification(notification: any): { status: number; body: string } {
    return {
      status: 200,
      body: notification.challenge,
    };
  }

  handleRevocation(notification: any): { status: number; body: null } {
    this.eventProcessingService.handleRevocation(notification);
    return {
      status: 204,
      body: null,
    };
  }
}

export default WebhookService;
