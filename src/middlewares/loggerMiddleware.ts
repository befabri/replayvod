import winston from "winston";
import expressWinston from "express-winston";

const { label } = winston.format;

const combinedLogFormat = winston.format.printf((info) => {
    const { level, message, timestamp, label, ...rest } = info;

    let additionalInfo = "";
    if (rest.meta && rest.meta.req) {
        const { headers } = rest.meta.req;
        const ip = headers["x-forwarded-for"] || rest.meta.req.ip;
        additionalInfo = ` - ${ip} : ${headers["user-agent"]}`;
    }

    return `${timestamp} ${label} [${level}]: ${message}${additionalInfo}`;
});

const jsonFormat = winston.format.combine(
    winston.format.timestamp(),
    winston.format.json(),
    winston.format.prettyPrint()
);

const logger = winston.createLogger({
    level: "info",
    transports: [
        new winston.transports.File({
            filename: "log/error.log",
            level: "error",
            format: jsonFormat,
        }),
        new winston.transports.File({
            filename: "log/info.log",
            format: jsonFormat,
        }),
        new winston.transports.File({
            filename: "log/combined.log",
            format: winston.format.combine(
                label({ label: "Main" }),
                winston.format.timestamp(),
                combinedLogFormat
            ),
        }),
        new winston.transports.File({
            filename: "log/youtubedl.log",
            format: winston.format.combine(
                label({ label: "YoutubeDL" }),
                winston.format.timestamp(),
                combinedLogFormat
            ),
        }),
        new winston.transports.File({
            filename: "log/fixvideos.log",
            format: winston.format.combine(
                label({ label: "FixVideos" }),
                winston.format.timestamp(),
                combinedLogFormat
            ),
        }),
        new winston.transports.File({
            filename: "log/webhookevent.log",
            format: winston.format.combine(
                label({ label: "WebhookEvent" }),
                winston.format.timestamp(),
                combinedLogFormat
            ),
        }),
    ],
});

const requestLogger = expressWinston.logger({
    transports: [
        new winston.transports.Console({
            format: winston.format.combine(winston.format.colorize(), winston.format.simple()),
        }),
        new winston.transports.File({
            filename: "log/requests.log",
            format: jsonFormat,
        }),
        new winston.transports.File({
            filename: "log/combined.log",
            format: winston.format.combine(
                label({ label: "Request" }),
                winston.format.timestamp(),
                combinedLogFormat
            ),
        }),
    ],
    meta: true,
    msg: "HTTP {{req.method}} {{req.url}} - status: {{res.statusCode}} - response time: {{res.responseTime}}ms - user-agent: {{req.headers['user-agent']}} - client IP: {{req.ip}}",
    expressFormat: true,
});

const errorLogger = expressWinston.errorLogger({
    transports: [
        new winston.transports.Console({
            format: winston.format.combine(winston.format.colorize(), winston.format.simple()),
        }),
        new winston.transports.File({
            filename: "log/error.log",
            format: jsonFormat,
        }),
    ],
});

const youtubedlLogger = winston.createLogger({
    level: "info",
    transports: [
        new winston.transports.File({
            filename: "log/youtubedl.log",
            format: jsonFormat,
        }),
    ],
});

const fixvideosLogger = winston.createLogger({
    level: "info",
    transports: [
        new winston.transports.File({
            filename: "log/fixvideos.log",
            format: jsonFormat,
        }),
    ],
});

const webhookEventLogger = winston.createLogger({
    level: "info",
    transports: [
        new winston.transports.File({
            filename: "log/webhookevent.log",
            format: jsonFormat,
        }),
    ],
});

export { logger, requestLogger, errorLogger, youtubedlLogger, fixvideosLogger, webhookEventLogger };
