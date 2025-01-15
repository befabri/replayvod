import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";

interface Params extends RouteGenericInterface {
    Params: {
        id?: string;
        quality?: string;
    };
}

export class DownloadHandler {
    downloadStream = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const service = req.server.download.service;
        const channelRepository = req.server.channel.repository;
        const userRepository = req.server.user.repository;
        const userId = userRepository.getUserIdFromSession(req);
        if (!userId) {
            return reply.status(401).send({ message: "Unauthorized" });
        }
        const broadcasterId = req.params.id;
        if (!broadcasterId) {
            return reply.status(400).send({ message: "Invalid broadcaster id" });
        }
        const channel = await channelRepository.getChannel(broadcasterId);
        if (!channel) {
            return reply.status(404).send({ message: "Channel not found" });
        }
        const stream = await channelRepository.getChannelStream(broadcasterId, userId);
        if (!stream) {
            return reply.status(400).send({ message: "Stream is offline" });
        }
        const pendingDownload = await service.findPendingJobByBroadcasterId(broadcasterId);
        if (pendingDownload) {
            return reply.status(400).send({
                message: "There is already a job running for this broadcaster.",
                jobId: pendingDownload.id,
            });
        }
        const jobDetails = service.getDownloadJobDetail(stream, [userId], channel, req.params.quality || "");
        const jobId = await service.handleDownload(jobDetails, broadcasterId);
        reply.send({ jobId: jobId });
    };

    getJobStatus = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        const jobId = req.params.id;
        const service = req.server.download.service;
        if (!jobId) {
            return reply.status(400).send({ message: "Invalid broadcaster id" });
        }
        const status = service.getDownloadStatus(jobId);
        if (status) {
            return reply.send({ status });
        }
        reply.status(404).send({ message: "Job not found" });
    };
}
