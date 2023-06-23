import { Request, Response, NextFunction } from "express";
import WebhookService from "../services/webhookService";
import { Webhook } from "../models/webhookModel";
import {
  HMAC_PREFIX,
  MESSAGE_TYPE,
  MESSAGE_TYPE_NOTIFICATION,
  MESSAGE_TYPE_REVOCATION,
  MESSAGE_TYPE_VERIFICATION,
  TWITCH_MESSAGE_SIGNATURE,
} from "../constants/twitchConstants";

const webhookService = new WebhookService();

export const addWebhook = async (req: Request, res: Response, next: NextFunction) => {
  try {
    const webhook: Webhook = { id: req.body.id, url: req.body.url } as Webhook;
    const addedWebhook = await webhookService.addWebhook(webhook);
    res.status(200).json({ data: addedWebhook });
  } catch (error) {
    next(error);
  }
};

export const removeWebhook = async (req: Request, res: Response, next: NextFunction) => {
  try {
    const removedWebhook = await webhookService.removeWebhook(req.body.id);
    if (removedWebhook) {
      res.status(200).json({ data: removedWebhook });
    } else {
      res.status(404).json({ error: "Webhook not found" });
    }
  } catch (error) {
    next(error);
  }
};

export const callbackWebhook = async (req: Request, res: Response, next: NextFunction) => {
  let secret = webhookService.getSecret();
  let message = webhookService.getHmacMessage(req);
  let hmac = HMAC_PREFIX + webhookService.getHmac(secret, message);

  let signature = req.headers[TWITCH_MESSAGE_SIGNATURE];
  if (typeof signature !== "string") {
    res.sendStatus(400);
    return;
  }

  if (true === webhookService.verifyMessage(hmac, signature)) {
    console.log("signatures match");

    let notification = req.body;
    let messageType = req.headers[MESSAGE_TYPE];
    let response;

    if (MESSAGE_TYPE_NOTIFICATION === messageType) {
      response = webhookService.handleNotification(notification);
    } else if (MESSAGE_TYPE_VERIFICATION === messageType) {
      response = webhookService.handleVerification(notification);
    } else if (MESSAGE_TYPE_REVOCATION === messageType) {
      response = webhookService.handleRevocation(notification);
    } else {
      res.sendStatus(400);
      return;
    }

    res.status(response.status);
    if (response.body) {
      res.send(response.body);
    } else {
      res.end();
    }
  } else {
    res.sendStatus(403);
  }
};
