import { FastifyReply, FastifyRequest, RouteGenericInterface } from "fastify";
import fs from "fs";
import { LOG_PATH } from "../../constants/constant.folder";

let logCache;

interface Params extends RouteGenericInterface {
    Params: {
        id: number;
    };
}

interface LogEntry {
    level: number;
    time: number;
    domain: string;
    service: string;
    msg: string;
}

const logLevels: { [key: number]: string } = {
    30: "Info",
    50: "Error",
};

export class LoggingHandler {
    private formatLog = (log: LogEntry): string => {
        const date = new Date(log.time);
        const formattedDate = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(
            date.getDate()
        ).padStart(2, "0")} ${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(
            2,
            "0"
        )}:${String(date.getSeconds()).padStart(2, "0")}.${String(date.getMilliseconds()).charAt(0)}`;

        const levelStr = logLevels[log.level] || "Info";

        return `${formattedDate}|${levelStr}|${log.domain}:${log.service}|${log.msg}`;
    };

    getLog = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        try {
            const logId = req.params.id;
            if (isNaN(logId)) {
                return reply.code(404).send({ message: `'${req.params.id}' not found` });
            }
            logCache = await fs.promises.readFile(LOG_PATH, "utf-8");
            reply.send(logCache);
        } catch (error) {
            reply.code(500).send({ message: "Internal server error" });
        }
    };

    getLogs = async (req: FastifyRequest, reply: FastifyReply) => {
        try {
            const repository = req.server.logging.repository;
            const logs = await repository.getAllLogs();
            reply.send(logs);
        } catch (error) {
            reply.code(500).send({ message: "Internal server error" });
        }
    };

    getDomain = async (req: FastifyRequest<Params>, reply: FastifyReply) => {
        try {
            const logId = req.params.id;
            const repository = req.server.logging.repository;

            if (isNaN(logId)) {
                return reply.code(404).send({ message: `'${req.params.id}' not found` });
            }

            const logDomain = await repository.getDomain(logId);

            if (!logDomain) {
                return reply.code(404).send({ message: `'${req.params.id}' not found` });
            }

            const fileContents = await fs.promises.readFile(LOG_PATH, "utf-8");
            const lines = fileContents.split("\n").filter((line) => line.trim().length > 0);
            const filteredLogs = lines
                .map((line) => JSON.parse(line))
                .filter((log) => log.domain === logDomain.domain);

            if (!filteredLogs.length) {
                return reply.code(404).send({ message: `Logs for service '${logDomain.domain}' not found` });
            }

            const formattedFilteredLogs = filteredLogs.map(this.formatLog).join("\n");
            reply.send(formattedFilteredLogs);
        } catch (error) {
            reply.code(500).send({ message: "Internal server error" });
        }
    };

    getDomains = async (req: FastifyRequest, reply: FastifyReply) => {
        try {
            const repository = req.server.logging.repository;
            const logs = await repository.getAllDomains();
            reply.send(logs);
        } catch (error) {
            reply.code(500).send({ message: "Internal server error" });
        }
    };
}
