import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import { jobService } from "@services";
import * as channelService from "@api/channel";
import * as downloadService from "./download";
import * as userService from "@api/user";
import * as videoService from "@api/video";
import TwitchAPI from "@integration/twitch/twitchAPI";
import { Quality } from "@prisma/client";
import { JobDetail } from "@sharedTypes";

const CALLBACK_URL_WEBHOOK = process.env.CALLBACK_URL_WEBHOOK;
const twitchAPI = new TwitchAPI();

interface Params extends RouteGenericInterface {
    Params: {
        id?: string;
        quality?: string;
    };
}

interface DownloadRequestBody extends RouteGenericInterface {
    Body: DownloadScheduleDTO;
}

interface DownloadScheduleDTO {
    channelName: string;
    quality: Quality;
    hasTags: boolean;
    tags?: string;
    hasMinView: boolean;
    viewersCount?: number | null;
    hasCategory: boolean;
    category: string;
    timeBeforeDelete?: number | null;
    isDeleteRediff: boolean;
    requestedBy?: string;
}

export const scheduleUser = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const userId = req.params.id;

    if (!userId || typeof userId !== "string") {
        reply.status(400).send("Invalid user id");
        return;
    }

    try {
        const result = await downloadService.planningRecord(userId);
        reply.send(result);
    } catch (error) {
        console.error("Error recording user:", error);
        reply.status(500).send("Error recording user");
    }
};

export const scheduleDownload = async (req: FastifyRequest<DownloadRequestBody>, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    if (!userId) {
        reply.status(401).send("Unauthorized");
        return;
    }
    const data = req.body;
    data.quality = videoService.mapVideoQualityToQuality(data.quality);
    // if (!data.source || !data.channelName || !data.trigger || !data.quality) {
    //     reply.status(400).send("Invalid request data");
    //     return;
    // }
    data.requestedBy = userId;
    const channel = await channelService.getChannelDetailByName(data.channelName);
    if (!channel) {
        reply.status(400).send("Invalid request data");
        return;
    }
    try {
        // Todo before check tags/categories and format tags
        await downloadService.addSchedule(data);
        reply.status(200).send("Schedule saved successfully.");
    } catch (error) {
        console.error("Error scheduling download:", error);
        reply.status(500).send("Error scheduling download.");
    }
};

export const downloadStream = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const userId = userService.getUserIdFromSession(req);
    if (!userId) {
        reply.status(401).send("Unauthorized");
        return;
    }
    const broadcasterId = req.params.id;
    const quality: Quality = videoService.mapVideoQualityToQuality(req.params.quality);
    if (!broadcasterId) {
        reply.status(400).send("Invalid broadcaster id");
        return;
    }
    const channel = await channelService.getChannelDetailDB(broadcasterId);
    if (!channel) {
        reply.status(404).send("User not found");
        return;
    }
    const stream = await channelService.getStream(broadcasterId, userId);
    if (!stream) {
        reply.status(400).send({ message: "Stream is offline" });
        return;
    }
    const pendingJob = await jobService.findPendingJobByBroadcasterId(broadcasterId);
    if (pendingJob) {
        reply.status(400).send({
            message: "There is already a job running for this broadcaster.",
            jobId: pendingJob.id,
        });
        return;
    }
    const jobId = jobService.createJobId();
    const jobDetails: JobDetail = { stream, userId, channel, jobId, quality };
    await downloadService.handleDownload(jobDetails, broadcasterId);
    reply.send({ jobId });
};

export const getJobStatus = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    const jobId = req.params.id;
    if (!jobId) {
        reply.status(400).send("Invalid broadcaster id");
        return;
    }
    const status = jobService.getJobStatus(jobId);
    if (status) {
        reply.send({ status });
    } else {
        reply.status(404).send("Job not found");
    }
};
