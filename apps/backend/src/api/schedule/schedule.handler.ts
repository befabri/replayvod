import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import { CreateScheduleDTO, ToggleScheduleStatusDTO } from "./schedule.dto";

interface scheduleId {
    id: number;
}
interface Params extends RouteGenericInterface {
    Params: scheduleId;
}

interface CreateScheduleBody extends RouteGenericInterface {
    Body: CreateScheduleDTO;
}

interface ToggleScheduleStatusBody extends RouteGenericInterface {
    Params: scheduleId;
    Body: ToggleScheduleStatusDTO;
}

interface EditScheduleBody extends RouteGenericInterface {
    Params: scheduleId;
    Body: CreateScheduleDTO;
}

export class ScheduleHandler {
    private isValidData = (data: CreateScheduleDTO) => {
        return !(
            (data.hasMinView && !data.viewersCount) ||
            (data.hasTags && !data.tags) ||
            (data.hasCategory && !data.categories)
        );
    };

    createSchedule = async (req: FastifyRequest<CreateScheduleBody>, reply: FastifyReply) => {
        const repository = req.server.schedule.repository;
        const userRepository = req.server.user.repository;
        const channelRepository = req.server.channel.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const data = req.body;
        if (!this.isValidData(data)) {
            return reply.status(400).send({ message: "Invalid request data" });
        }
        const channel = await channelRepository.getChannelByName(data.channelName);
        if (!channel) {
            return reply.status(400).send({ message: "Invalid request data" });
        }
        try {
            await repository.createSchedule(data, userId);
            reply.status(200).send({ message: "Schedule saved successfully." });
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    removeSchedule = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        const repository = req.server.schedule.repository;
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const scheduleId = req.params.id;
        if (!scheduleId) {
            return reply.status(401).send({ message: "Invalid request data" });
        }
        try {
            const schedule = await repository.getSchedule(scheduleId, userId);
            if (!schedule) {
                return reply.status(400).send({ message: "Invalid request data" });
            }
            const removed = await repository.removeSchedule(scheduleId);
            if (!removed) {
                return reply.status(200).send({ message: "Error removing schedule" });
            }
            reply.status(200).send({ message: "Schedule removed successfully" });
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    editSchedule = async (req: FastifyRequest<EditScheduleBody>, reply: FastifyReply) => {
        const userRepository = req.server.user.repository;
        const channelRepository = req.server.channel.repository;
        const repository = req.server.schedule.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const scheduleId = req.params.id;
        const data = req.body;
        if (!this.isValidData(data)) {
            return reply.status(400).send({ message: "Invalid request data" });
        }
        try {
            const schedule = await repository.getSchedule(scheduleId, userId);
            const channel = await channelRepository.getChannelByName(data.channelName);
            if (!schedule || !channel) {
                return reply.status(400).send({ message: "Invalid request data" });
            }
            await repository.editSchedule(scheduleId, data);
            reply.status(200).send({ message: "Schedule edited successfully." });
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    toggleScheduleStatus = async (req: FastifyRequest<ToggleScheduleStatusBody>, reply: FastifyReply) => {
        const repository = req.server.schedule.repository;
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const scheduleId = req.params.id;
        const { enable } = req.body;
        try {
            const schedule = await repository.getSchedule(scheduleId, userId);
            if (!schedule) {
                return reply.status(400).send({ message: "Invalid request data" });
            }
            if (schedule.isDisabled === enable) {
                return reply.status(200).send({ message: "Schedule is already in the desired state" });
            }
            await repository.toggleSchedule(scheduleId, enable);
            reply.status(200).send({ message: "Schedule updated successfully" });
        } catch (error) {
            reply.status(500).send({ message: "Internal server error" });
        }
    };

    getCurrentSchedules = async (req: FastifyRequest, reply: FastifyReply) => {
        const repository = req.server.schedule.repository;
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const schedules = await repository.getCurrentSchedulesByUser(userId);
        reply.status(200).send(schedules);
    };
}
