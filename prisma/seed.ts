import { PrismaClient, Prisma } from "@prisma/client";

const prisma = new PrismaClient();

// @ts-ignore
const logData: Prisma.LogCreateInput[] = [
    {
        downloadUrl: "log/combined.log",
        filename: "combined.log",
        type: "Combined",
        lastWriteTime: new Date("1900-01-01T00:00:00Z"),
    },
    {
        downloadUrl: "log/youtubedl.log",
        filename: "youtubedl.log",
        type: "YoutubeDl",
        lastWriteTime: new Date("1900-01-01T00:00:00Z"),
    },
    {
        downloadUrl: "log/fixvideos.log",
        filename: "fixvideos.log",
        type: "FixVideos",
        lastWriteTime: new Date("1900-01-01T00:00:00Z"),
    },
    {
        downloadUrl: "log/requests.log",
        filename: "requests.log",
        type: "Requests",
        lastWriteTime: new Date("1900-01-01T00:00:00Z"),
    },
    {
        downloadUrl: "log/errors.log",
        filename: "errors.log",
        type: "Errors",
        lastWriteTime: new Date("1900-01-01T00:00:00Z"),
    },
    {
        downloadUrl: "log/infos.log",
        filename: "infos.log",
        type: "Infos",
        lastWriteTime: new Date("1900-01-01T00:00:00Z"),
    },
    {
        downloadUrl: "log/webhookevents.log",
        filename: "webhookevents.log",
        type: "WebhookEvents",
        lastWriteTime: new Date("1900-01-01T00:00:00Z"),
    },
];

// @ts-ignore
const taskData: Prisma.TaskCreateInput[] = [
    {
        id: "generateMissingThumbnail",
        name: "Generate Missing Thumbnail",
        description: "Generates missing thumbnails for videos",
        taskType: "generateMissingThumbnail",
        interval: 600000,
        lastDuration: 0,
        lastExecution: new Date("1900-01-01T00:00:00Z"),
        nextExecution: new Date("1900-01-01T00:00:00Z"),
    },
    {
        id: "fixMalformedVideos",
        name: "Fix Malformed Videos",
        description: "Fix Malformed Videos",
        taskType: "fixMalformedVideos",
        interval: 300000,
        lastDuration: 0,
        lastExecution: new Date("1900-01-01T00:00:00Z"),
        nextExecution: new Date("1900-01-01T00:00:00Z"),
    },
    {
        id: "subToAllStreamEventFollowed",
        name: "Sub to All Stream Event Followed",
        description: "Sub to All Stream Event Followed",
        taskType: "subToAllStreamEventFollowed",
        interval: 300000,
        lastDuration: 0,
        lastExecution: new Date("1900-01-01T00:00:00Z"),
        nextExecution: new Date("1900-01-01T00:00:00Z"),
    },
];

async function main() {
    console.log(`Start seeding ...`);
    for (const u of logData) {
        const log = await prisma.log.create({
            data: u,
        });
        console.log(`Created log with id: ${log.id}`);
    }
    for (const u of taskData) {
        const task = await prisma.task.create({
            data: u,
        });
        console.log(`Created task with id: ${task.id}`);
    }
    console.log(`Seeding finished.`);
}

main()
    .then(async () => {
        await prisma.$disconnect();
    })
    .catch(async (e) => {
        console.error(e);
        await prisma.$disconnect();
        process.exit(1);
    });
