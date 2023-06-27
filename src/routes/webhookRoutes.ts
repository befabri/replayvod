import express, { Router } from "express";
import * as webhookController from "../controllers/webhookController";

const router: Router = express.Router();

router.post("/webhooks", webhookController.addWebhook);
router.delete("/webhooks", webhookController.removeWebhook);
router.post("/webhooks/callback", webhookController.callbackWebhook);

export default router;
