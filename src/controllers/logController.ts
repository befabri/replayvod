import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import { logService } from "../services";
import { Log } from "../models/logModel";
import fs from "fs";
import path from "path";

interface Params extends RouteGenericInterface {
    Params: {
        id: number;
    };
}

export const getLogs = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const logs = await logService.getAllLogs();
        reply.send(logs);
    } catch (error) {
        reply.code(500).send("Internal server error");
    }
};

export const getLog = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    try {
        const logId = req.params.id;
        if (isNaN(logId)) {
            reply.code(404).send({ message: `'${req.params.id}' not found` });
            return;
        }
        const logDir = process.env.LOG_DIR || "."; // TODO
        const log: Log = (await logService.getLog(logId)) as Log;
        const logPath = path.resolve(logDir, log.filename);
        fs.stat(logPath, (err, stat) => {
            if (err) {
                if (err.code === "ENOENT") {
                    return reply.code(404).send("File not found");
                } else {
                    return reply.code(500).send("Error accessing the file");
                }
            }
            const stream = fs.createReadStream(logPath);
            stream.on("open", () => {
                reply.header("Content-Type", "text/plain");
                reply.header("Content-Length", String(stat.size));
                reply.send(stream);
            });
            stream.on("error", (streamErr) => {
                return reply.code(500).send(`Error streaming the log file: ${streamErr.message}`);
            });
        });
    } catch (error) {
        reply.code(500).send("Internal server error");
    }
};
