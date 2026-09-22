import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import type { StorageState } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { resyncQuery } from "@/lib/query";
import { withSessionProbe } from "@/stores/auth";

export function storageUnreadable(state: StorageState | undefined) {
	return state === "unattached" || state === "unreachable";
}

export function storageUnwritable(state: StorageState | undefined) {
	return state === "read_only" || state === "full" || storageUnreadable(state);
}

export function useStorageStatus() {
	const trpc = useTRPC();
	return useQuery(
		trpc.storage.status.queryOptions(undefined, {
			staleTime: 30_000,
			refetchOnWindowFocus: true,
		}),
	);
}

export function useStorageDetails(enabled = true) {
	const trpc = useTRPC();
	return useQuery(trpc.storage.details.queryOptions(undefined, { enabled }));
}

function useInvalidateStorage() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return () => resyncQuery(queryClient, trpc.storage.pathKey());
}

export function useAdoptStorage() {
	const trpc = useTRPC();
	const invalidate = useInvalidateStorage();
	return useMutation(
		trpc.storage.adopt.mutationOptions({ onSuccess: invalidate }),
	);
}

export function useLiveStorageStatus() {
	const trpc = useTRPC();
	const invalidate = useInvalidateStorage();
	useSubscription({
		...trpc.storage.statusLive.subscriptionOptions(),
		onStarted: invalidate,
		onData: invalidate,
		onError: withSessionProbe(),
		onConnectionStateChange: (connection) => {
			if (connection.state === "connecting" && connection.error) {
				withSessionProbe()(connection.error);
			}
		},
	});
}
