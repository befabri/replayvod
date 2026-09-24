import {
	keepPreviousData,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useTRPC } from "@/api/trpc";
import { resyncQuery } from "@/lib/query";
import { withSessionProbe } from "@/stores/auth";

export function useEventLogs(params: {
	limit: number;
	offset: number;
	domain?: string;
	severity?: string;
}) {
	const trpc = useTRPC();
	return useQuery(
		trpc.system.eventLogs.queryOptions(
			{
				limit: params.limit,
				offset: params.offset,
				domain: params.domain ?? "",
				severity: params.severity ?? "",
			},
			{ placeholderData: keepPreviousData },
		),
	);
}

export function useLiveSystemEvents() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	useSubscription({
		...trpc.system.events.subscriptionOptions(),
		onStarted: () => resyncQuery(queryClient, trpc.system.eventLogs.pathKey()),
		onData: () => resyncQuery(queryClient, trpc.system.eventLogs.pathKey()),
		onError: withSessionProbe(),
	});
}
