import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useTRPC } from "@/api/trpc";

export function useEventLogs(params: {
	limit: number;
	offset: number;
	domain?: string;
	severity?: string;
}) {
	const trpc = useTRPC();
	return useQuery(
		trpc.system.eventLogs.queryOptions({
			limit: params.limit,
			offset: params.offset,
			domain: params.domain ?? "",
			severity: params.severity ?? "",
		}),
	);
}

// useLiveSystemEvents subscribes to the system.events SSE feed and
// invalidates the event_logs query set on every new row. A full
// invalidation (not per-page patching) keeps filter + pagination
// coherent — if we patched one page optimistically, a user on page 2
// would see a phantom new row that doesn't actually belong there.
export function useLiveSystemEvents() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	useSubscription({
		...trpc.system.events.subscriptionOptions(),
		onData: () => {
			queryClient.invalidateQueries({
				queryKey: trpc.system.eventLogs.queryKey(),
			});
		},
	});
}
