import { v4 as uuidv4 } from "uuid";
import { prisma } from "../server";

const CACHE_MINUTES = 10 * 60 * 1000;

enum cacheType {
    STREAM = "stream",
    VIDEO = "video",
    FOLLOWED_CHANNELS = "followedChannels",
    EVENT_SUB = "eventSub",
    FOLLOWED_STREAMS = "followedStreams",
}

enum SortOrder {
    ASC = "asc",
    DESC = "desc",
}

interface FetchLogWhereClause {
    userId: string;
    fetchType: cacheType;
    broadcasterId?: string;
}

interface FetchLogQuery {
    where: FetchLogWhereClause;
    orderBy: {
        fetchedAt: SortOrder;
    };
}

interface FetchLogCreateInput {
    id: string;
    userId: string;
    fetchedAt: Date;
    fetchType: cacheType;
    broadcasterId?: string;
}

async function getLastFetch(params: {
    fetchType: cacheType.STREAM;
    userId: string;
    broadcasterId: string;
}): Promise<ReturnType<typeof prisma.fetchLog.findFirst>>;

async function getLastFetch(params: {
    fetchType: Exclude<cacheType, cacheType.STREAM>;
    userId: string;
}): Promise<ReturnType<typeof prisma.fetchLog.findFirst>>;

async function getLastFetch({
    fetchType,
    userId,
    broadcasterId,
}: {
    fetchType: cacheType;
    userId: string;
    broadcasterId?: string;
}): Promise<ReturnType<typeof prisma.fetchLog.findFirst>> {
    if (fetchType !== cacheType.STREAM && broadcasterId) {
        throw new Error("broadcasterId should not be provided for fetchTypes other than STREAM");
    }
    let query: FetchLogQuery = {
        where: {
            userId: userId,
            fetchType: fetchType,
        },
        orderBy: {
            fetchedAt: SortOrder.DESC,
        },
    };

    if (fetchType === cacheType.STREAM) {
        if (!broadcasterId) {
            throw new Error("broadcasterId is required for fetchType STREAM");
        }
        query.where.broadcasterId = broadcasterId;
    }

    const fetchLog = await prisma.fetchLog.findFirst(query);
    return fetchLog;
}

async function createFetch(params: {
    fetchType: cacheType.STREAM;
    userId: string;
    broadcasterId: string;
}): Promise<ReturnType<typeof prisma.fetchLog.create>>;

async function createFetch(params: {
    fetchType: Exclude<cacheType, cacheType.STREAM>;
    userId: string;
}): Promise<ReturnType<typeof prisma.fetchLog.create>>;

async function createFetch({
    fetchType,
    userId,
    broadcasterId,
}: {
    fetchType: cacheType;
    userId: string;
    broadcasterId?: string;
}): Promise<ReturnType<typeof prisma.fetchLog.create>> {
    if (fetchType !== cacheType.STREAM && broadcasterId) {
        throw new Error("broadcasterId should not be provided for fetchTypes other than STREAM");
    }
    if (fetchType === cacheType.STREAM && !broadcasterId) {
        throw new Error("broadcasterId is required for fetchType STREAM");
    }

    let data: FetchLogCreateInput = {
        id: generateFetchId(),
        userId: userId,
        fetchedAt: new Date(),
        fetchType: fetchType,
    };

    if (broadcasterId) {
        data.broadcasterId = broadcasterId;
    }

    return await prisma.fetchLog.create({ data });
}

const isCacheExpire = (fetchedAt: Date) => {
    return fetchedAt > new Date(Date.now() - CACHE_MINUTES);
};

const generateFetchId = () => {
    return uuidv4();
};

export default {
    isCacheExpire,
    createFetch,
    getLastFetch,
    cacheType,
};
