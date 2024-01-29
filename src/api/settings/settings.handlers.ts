import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { SettingsDTO, transformSettings } from "./settings.DTO";
import { settingFeature } from ".";
import { userFeature } from "../user";

interface SettingsRequestBody extends RouteGenericInterface {
    Body: SettingsDTO;
}
export const getSettings = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    try {
        const settings = await settingFeature.getSettings(userId);
        reply.send(settings);
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const upsertSettings = async (req: FastifyRequest<SettingsRequestBody>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const data = req.body;
    try {
        const { settings } = await transformSettings(data, userId);
        const message = await settingFeature.addSettings(settings);
        reply.status(200).send(message);
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};
