export interface EventSubDTO {
    data: {
        cost: {
            total: number;
            total_cost: number;
            max_total_cost: number;
        };
        list: {
            id: string;
            status: string;
            subscriptionType: string;
            broadcasterId: string;
            createdAt: Date;
            cost: number;
        }[];
    } | null;
    message: string;
}
