import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";
import { invalidateCaches } from "@/lib/query";
import { eventsubCaches } from "./cache";

export function useEventSubConfig(options?: { enabled?: boolean }) {
	const trpc = useTRPC();
	return useQuery(
		trpc.eventsub.config.queryOptions(undefined, {
			enabled: options?.enabled ?? true,
		}),
	);
}

export function useUpdateEventSubConfig() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = eventsubCaches(trpc);
	return useMutation(
		trpc.eventsub.updateConfig.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches, ["config"]),
		}),
	);
}

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
	const caches = eventsubCaches(trpc);
	return useMutation(
		trpc.eventsub.snapshot.mutationOptions({
			onSuccess: () =>
				invalidateCaches(queryClient, caches, [
					"latestSnapshot",
					"listSnapshots",
					"listSubscriptions",
				]),
		}),
	);
}

export function useUnsubscribe() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = eventsubCaches(trpc);
	return useMutation(
		trpc.eventsub.unsubscribe.mutationOptions({
			onSuccess: () =>
				invalidateCaches(queryClient, caches, ["listSubscriptions"]),
		}),
	);
}
