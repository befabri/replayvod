import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";

export function useSubscriptions(limit = 50, offset = 0) {
	const trpc = useTRPC();
	return useQuery(
		trpc.eventsub.listSubscriptions.queryOptions({ limit, offset }),
	);
}

export function useSnapshots(limit = 30, offset = 0) {
	const trpc = useTRPC();
	return useQuery(trpc.eventsub.listSnapshots.queryOptions({ limit, offset }));
}

export function useLatestSnapshot() {
	const trpc = useTRPC();
	return useQuery(trpc.eventsub.latestSnapshot.queryOptions());
}

export function useSnapshotNow() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.eventsub.snapshot.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.eventsub.latestSnapshot.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.eventsub.listSnapshots.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.eventsub.listSubscriptions.queryKey(),
				});
			},
		}),
	);
}

export function useUnsubscribe() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.eventsub.unsubscribe.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.eventsub.listSubscriptions.queryKey(),
				});
			},
		}),
	);
}
