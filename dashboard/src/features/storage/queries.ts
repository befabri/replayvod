import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import type { StorageState } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { resyncQuery } from "@/lib/query";
import { withSessionProbe } from "@/stores/auth";

// Storage that cannot be trusted for reads: media does not play and nothing
// is tombstoned. Read-only storage still serves, it only pauses recording.
export function storageUnreadable(state: StorageState | undefined) {
	return state === "unattached" || state === "unreachable";
}

// Unknown is not a verdict. Only a confirmed degraded state blocks writes.
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

// useStorageDetails is owner-only on the server; callers gate it on role.
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

// useLiveStorageStatus refetches the status and details on every readiness
// transition pushed by the server, so the banner appears and clears without
// polling. The feed does not replay missed events, so every connection also
// resynchronizes the queries, including a reconnect after a missed transition.
export function useLiveStorageStatus() {
	const trpc = useTRPC();
	const invalidate = useInvalidateStorage();
	useSubscription({
		...trpc.storage.statusLive.subscriptionOptions(),
		onStarted: invalidate,
		onData: invalidate,
		onError: withSessionProbe(),
		// A refused WebSocket handshake has no tRPC error frame. Probe the session
		// on connection failures as well so a 401 still redirects to login.
		onConnectionStateChange: (connection) => {
			if (connection.state === "connecting" && connection.error) {
				withSessionProbe()(connection.error);
			}
		},
	});
}
