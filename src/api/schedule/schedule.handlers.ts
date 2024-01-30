import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import { CreateScheduleDTO, ToggleScheduleStatusDTO } from "./schedule.DTO";
import { userFeature } from "../user";
import { channelFeature } from "../channel";
import { downloadFeature } from ".";

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

const isValidData = (data: CreateScheduleDTO) => {
    return !(
        (data.hasMinView && !data.viewersCount) ||
        (data.hasTags && !data.tags) ||
        (data.hasCategory && !data.categories)
    );
};

export const createSchedule = async (req: FastifyRequest<CreateScheduleBody>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const data = req.body;
    if (!isValidData(data)) {
        return reply.status(400).send({ message: "Invalid request data" });
    }
    const channel = await channelFeature.getChannelByName(data.channelName);
    if (!channel) {
        return reply.status(400).send({ message: "Invalid request data" });
    }
    try {
        await downloadFeature.createSchedule(data, userId);
        reply.status(200).send({ message: "Schedule saved successfully." });
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const removeSchedule = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const scheduleId = req.params.id;
    if (!scheduleId) {
        return reply.status(401).send({ message: "Invalid request data" });
    }
    try {
        const schedule = await downloadFeature.getSchedule(scheduleId, userId);
        if (!schedule) {
            return reply.status(400).send({ message: "Invalid request data" });
        }
        const removed = await downloadFeature.removeSchedule(scheduleId);
        if (!removed) {
            return reply.status(200).send({ message: "Error removing schedule" });
        }
        reply.status(200).send({ message: "Schedule removed successfully" });
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const editSchedule = async (req: FastifyRequest<EditScheduleBody>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const scheduleId = req.params.id;
    const data = req.body;
    if (!isValidData(data)) {
        return reply.status(400).send({ message: "Invalid request data" });
    }
    try {
        const schedule = await downloadFeature.getSchedule(scheduleId, userId);
        const channel = await channelFeature.getChannelByName(data.channelName);
        if (!schedule || !channel) {
            return reply.status(400).send({ message: "Invalid request data" });
        }
        await downloadFeature.editSchedule(scheduleId, data);
        reply.status(200).send({ message: "Schedule edited successfully." });
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const toggleScheduleStatus = async (req: FastifyRequest<ToggleScheduleStatusBody>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const scheduleId = req.params.id;
    const { enable } = req.body;
    try {
        const schedule = await downloadFeature.getSchedule(scheduleId, userId);
        if (!schedule) {
            return reply.status(400).send({ message: "Invalid request data" });
        }
        if (schedule.isDisabled === enable) {
            return reply.status(200).send({ message: "Schedule is already in the desired state" });
        }
        await downloadFeature.toggleSchedule(scheduleId, enable);
        reply.status(200).send({ message: "Schedule updated successfully" });
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const getCurrentSchedules = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const schedules = await downloadFeature.getCurrentSchedulesByUser(userId);
    reply.status(200).send(schedules);
};
