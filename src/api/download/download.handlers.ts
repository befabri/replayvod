import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import { jobService } from "../../services";
import { Quality } from "@prisma/client";
import { JobDetail } from "../../types/sharedTypes";
import { logger as rootLogger } from "../../app";
import { DownloadScheduleDTO, ScheduleToggleDTO } from "./download.DTO";
import { userFeature } from "../user";
import { channelFeature } from "../channel";
import { videoFeature } from "../video";
import { downloadFeature } from ".";
const logger = rootLogger.child({ domain: "download", service: "downloadHandler" });

const CALLBACK_URL_WEBHOOK = process.env.CALLBACK_URL_WEBHOOK;

interface Params extends RouteGenericInterface {
    Params: {
        id?: string;
        quality?: string;
        scheduleId?: number;
    };
}

interface DownloadRequestBody extends RouteGenericInterface {
    Body: DownloadScheduleDTO;
}

interface ScheduleToggleBody extends RouteGenericInterface {
    Params: {
        scheduleId: number;
    };
    Body: ScheduleToggleDTO;
}

interface ScheduleEditRequestBody extends RouteGenericInterface {
    Params: {
        scheduleId: number;
    };
    Body: DownloadScheduleDTO;
}

const isValidData = (data: DownloadScheduleDTO) => {
    return !(
        (data.hasMinView && !data.viewersCount) ||
        (data.hasTags && !data.tag) ||
        (data.hasCategory && !data.category)
    );
};

export const scheduleDownload = async (req: FastifyRequest<DownloadRequestBody>, reply: FastifyReply) => {
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
        await downloadFeature.addSchedule(data, userId);
        reply.status(200).send({ message: "Schedule saved successfully." });
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const scheduleRemoved = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const scheduleId = req.params.scheduleId;
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

export const scheduleDownloadEdit = async (req: FastifyRequest<ScheduleEditRequestBody>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const scheduleId = req.params.scheduleId;
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

export const scheduleToggle = async (req: FastifyRequest<ScheduleToggleBody>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const scheduleId = req.params.scheduleId;
    const { enable } = req.body;
    try {
        const schedule = await downloadFeature.getSchedule(scheduleId, userId);
        if (!schedule) {
            return reply.status(400).send({ message: "Invalid request data" });
        }
        if (schedule.isDisabled === enable) {
            return reply.status(200).send({ message: "Schedule is already in the desired state" });
        }
        await downloadFeature.toggleDownloadSchedule(scheduleId, enable);
        reply.status(200).send({ message: "Schedule updated successfully" });
    } catch (error) {
        reply.status(500).send({ message: "Internal server error" });
    }
};

export const downloadStream = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const broadcasterId = req.params.id;
    if (!broadcasterId) {
        return reply.status(400).send({ message: "Invalid broadcaster id" });
    }
    const channel = await channelFeature.getChannel(broadcasterId);
    if (!channel) {
        return reply.status(404).send({ message: "Channel not found" });
    }
    const stream = await channelFeature.getChannelStream(broadcasterId, userId);
    if (!stream) {
        return reply.status(400).send({ message: "Stream is offline" });
    }
    const pendingJob = await jobService.findPendingJobByBroadcasterId(broadcasterId);
    if (pendingJob) {
        return reply.status(400).send({
            message: "There is already a job running for this broadcaster.",
            jobId: pendingJob.id,
        });
    }
    const jobId = jobService.createJobId();
    const quality: Quality = videoFeature.mapVideoQualityToQuality(req.params.quality || "");
    const jobDetails: JobDetail = { stream, userId, channel, jobId, quality };
    await downloadFeature.handleDownload(jobDetails, broadcasterId);
    reply.send({ jobId });
};

export const getCurrentSchedule = async (req: FastifyRequest, reply: FastifyReply) => {
    const userId = userFeature.getUserIdFromSession(req);
    if (!userId) {
        return reply.status(401).send({ message: "Unauthorized" });
    }
    const schedule = await downloadFeature.getCurrentScheduleByUser(userId);
    reply.send(schedule);
};

export const getJobStatus = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const jobId = req.params.id;
    if (!jobId) {
        return reply.status(400).send({ message: "Invalid broadcaster id" });
    }
    const status = jobService.getJobStatus(jobId);
    if (status) {
        return reply.send({ status });
    }
    reply.status(404).send({ message: "Job not found" });
};
