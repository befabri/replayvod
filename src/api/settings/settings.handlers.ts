import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { logger as rootLogger } from "../../app";
import { SettingsDTO, transformSettings } from "./settings.DTO";
import { settingFeature } from ".";
import { userFeature } from "../user";
const logger = rootLogger.child({ domain: "settings", service: "settingsHandler" });

interface SettingsRequestBody extends RouteGenericInterface {
    Body: SettingsDTO;
}
export const getSettings = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        reply.status(401).send("Unauthorized");
        return;
    }
    try {
        const settings = await settingFeature.getSettings(userId);
        reply.send(settings);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};

export const upsertSettings = async (req: FastifyRequest<SettingsRequestBody>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        reply.status(401).send("Unauthorized");
        return;
    }
    const data = req.body;
    try {
        const { settings } = await transformSettings(data, userId);
        const message = await settingFeature.addSettings(settings);
        reply.status(200).send(message);
    } catch (error) {
        reply.status(500).send("Internal server error");
    }
};
