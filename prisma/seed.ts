import { PrismaClient, Prisma } from "@prisma/client";

const prisma = new PrismaClient();

// @ts-ignore
const logData: Prisma.LogCreateInput[] = [
    {
        downloadUrl: "logs/replay.log",
        filename: "replay.log",
        lastWriteTime: new Date("1900-01-01T00:00:00Z"),
    },
];

const eventLogData: Prisma.EventLogCreateInput[] = [
    {
        domain: "auth",
    },
    {
        domain: "channel",
    },
    {
        domain: "video",
    },
    {
        domain: "download",
    },
    {
        domain: "webhook",
    },
    {
        domain: "task",
    },
    {
        domain: "twitch",
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
    for (const u of eventLogData) {
        const domain = await prisma.eventLog.create({
            data: u,
        });
        console.log(`Created task with id: ${domain.id}`);
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
