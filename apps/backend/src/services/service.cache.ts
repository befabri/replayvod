import { PrismaClient } from "@prisma/client";
import { v4 as uuidv4 } from "uuid";

const CACHE_MINUTES = 10 * 60 * 1000;

export enum cacheType {
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

export class CacheService {
    constructor(private db: PrismaClient) {}

    async getLastFetch(params: {
        fetchType: cacheType.STREAM;
        userId: string;
        broadcasterId: string;
    }): Promise<ReturnType<typeof this.db.fetchLog.findFirst>>;

    async getLastFetch(params: {
        fetchType: Exclude<cacheType, cacheType.STREAM>;
        userId: string;
    }): Promise<ReturnType<typeof this.db.fetchLog.findFirst>>;

    async getLastFetch({
        fetchType,
        userId,
        broadcasterId,
    }: {
        fetchType: cacheType;
        userId: string;
        broadcasterId?: string;
    }): Promise<ReturnType<typeof this.db.fetchLog.findFirst>> {
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

        const fetchLog = await this.db.fetchLog.findFirst(query);
        return fetchLog;
    }

    async createFetch(params: {
        fetchType: cacheType.STREAM;
        userId: string;
        broadcasterId: string;
    }): Promise<ReturnType<typeof this.db.fetchLog.create>>;

    async createFetch(params: {
        fetchType: Exclude<cacheType, cacheType.STREAM>;
        userId: string;
    }): Promise<ReturnType<typeof this.db.fetchLog.create>>;

    async createFetch({
        fetchType,
        userId,
        broadcasterId,
    }: {
        fetchType: cacheType;
        userId: string;
        broadcasterId?: string;
    }): Promise<ReturnType<typeof this.db.fetchLog.create>> {
        if (fetchType !== cacheType.STREAM && broadcasterId) {
            throw new Error("broadcasterId should not be provided for fetchTypes other than STREAM");
        }
        if (fetchType === cacheType.STREAM && !broadcasterId) {
            throw new Error("broadcasterId is required for fetchType STREAM");
        }

        let data: FetchLogCreateInput = {
            id: this.generateFetchId(),
            userId: userId,
            fetchedAt: new Date(),
            fetchType: fetchType,
        };

        if (broadcasterId) {
            data.broadcasterId = broadcasterId;
        }

        return await this.db.fetchLog.create({ data });
    }

    isCacheExpire = (fetchedAt: Date) => {
        return fetchedAt > new Date(Date.now() - CACHE_MINUTES);
    };

    generateFetchId = () => {
        return uuidv4();
    };
}
