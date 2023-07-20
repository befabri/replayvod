import { Request, Response } from "express";
import { logService } from "../services";
import { Log } from "../models/logModel";
import fs from "fs";
import path from "path";

export const getLogs = async (req: Request, res: Response) => {
    try {
        const logs = await logService.getAllLogs();
        res.json(logs);
    } catch (error) {
        res.status(500).send("Internal server error");
    }
};

export const getLog = async (req: Request, res: Response) => {
    try {
        const logId = parseInt(req.params.id, 10);
        if (isNaN(logId)) {
            res.status(404).send({ message: `'${req.params.id}' not found` });
            return;
        }
        const logDir = process.env.LOG_DIR;
        const log: Log = (await logService.getLog(logId)) as Log;
        const logPath = path.resolve(logDir, log.filename);
        fs.stat(logPath, (err, stat) => {
            if (err) {
                if (err.code === "ENOENT") {
                    return res.status(404).send("File not found");
                } else {
                    return res.status(500).send("Error accessing the file");
                }
            }
            const stream = fs.createReadStream(logPath);
            stream.on("open", () => {
                res.set("Content-Type", "text/plain");
                res.set("Content-Length", String(stat.size));
                stream.pipe(res);
            });
            stream.on("error", (streamErr) => {
                return res.status(500).send(`Error streaming the log file: ${streamErr.message}`);
            });
        });
    } catch (error) {
        res.status(500).send("Internal server error");
    }
};
