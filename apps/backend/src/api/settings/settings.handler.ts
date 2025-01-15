import { FastifyRequest, FastifyReply, RouteGenericInterface } from "fastify";
import { SettingsDTO, transformSettings } from "./settings.dto";

interface SettingsRequestBody extends RouteGenericInterface {
    Body: SettingsDTO;
}

export class SettingsHandler {
    getSettings = async (req: FastifyRequest, reply: FastifyReply) => {
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        const repository = req.server.settings.repository;
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        try {
            const settings = await repository.getSettings(userId);
            reply.send(settings);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    upsertSettings = async (req: FastifyRequest<SettingsRequestBody>, reply: FastifyReply) => {
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        const repository = req.server.settings.repository;
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const data = req.body;
        try {
            const { settings } = await transformSettings(data, userId);
            const message = await repository.addSettings(settings);
            reply.status(200).send(message);
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };
}
