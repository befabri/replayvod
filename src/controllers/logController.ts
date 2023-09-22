import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import { logService } from "../services";
import fs from "fs";
import path from "path";
import { EventLog } from "@prisma/client";

let logCache;

interface Params extends RouteGenericInterface {
    Params: {
        id: number;
    };
}

const logLevels = {
    30: "Info",
    50: "Error",
};

const formatLog = (log) => {
    const date = new Date(log.time);
    const formattedDate = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(
        date.getDate()
    ).padStart(2, "0")} ${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(
        2,
        "0"
    )}:${String(date.getSeconds()).padStart(2, "0")}.${String(date.getMilliseconds()).charAt(0)}`;

    const levelStr = logLevels[log.level] || "";

    return `${formattedDate}|${levelStr}|${log.domain}:${log.service}|${log.msg}`;
};

export const getLog = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    try {
        const logId = req.params.id;
        if (isNaN(logId)) {
            reply.code(404).send({ message: `'${req.params.id}' not found` });
            return;
        }
        const logDir = process.env.LOG_DIR || "."; // TODO
        const logFilePath = path.resolve(logDir, "replay.log");
        logCache = await fs.promises.readFile(logFilePath, "utf-8");
        reply.send(logCache);
    } catch (error) {
        reply.code(500).send("Internal server error");
    }
};

export const getLogs = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const logs = await logService.getAllLogs();
        reply.send(logs);
    } catch (error) {
        reply.code(500).send("Internal server error");
    }
};

export const getDomain = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
    try {
        const logId = req.params.id;

        if (isNaN(logId)) {
            return reply.code(404).send({ message: `'${req.params.id}' not found` });
        }

        const logDomain: EventLog = await logService.getDomain(logId);

        if (!logDomain) {
            return reply.code(404).send({ message: `'${req.params.id}' not found` });
        }

        const logDir = process.env.LOG_DIR || ".";
        const logFilePath = path.resolve(logDir, "replay.log");
        const fileContents = await fs.promises.readFile(logFilePath, "utf-8");
        const lines = fileContents.split("\n").filter((line) => line.trim().length > 0);
        const filteredLogs = lines
            .map((line) => JSON.parse(line))
            .filter((log) => log.domain === logDomain.domain);

        if (!filteredLogs.length) {
            return reply.code(404).send({ message: `Logs for service '${logDomain.domain}' not found` });
        }

        const formattedFilteredLogs = filteredLogs.map(formatLog).join("\n");
        reply.send(formattedFilteredLogs);
    } catch (error) {
        return reply.code(500).send("Internal server error");
    }
};

export const getDomains = async (req: FastifyRequest, reply: FastifyReply) => {
    try {
        const logs = await logService.getAllDomains();
        reply.send(logs);
    } catch (error) {
        reply.code(500).send("Internal server error");
    }
};
