import { hashKey, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import {
	useCallback,
	useMemo,
	useRef,
	useState,
	useSyncExternalStore,
} from "react";
import type { StreamLiveEvent } from "@/api/generated/trpc";
import { useTRPC, useTRPCClient } from "@/api/trpc";
import { resyncQuery } from "@/lib/query";
import { withSessionProbe } from "@/stores/auth";

export function useFollowedStreams() {
	const trpc = useTRPC();
	return useQuery(
		trpc.stream.followed.queryOptions(undefined, { staleTime: 30_000 }),
	);
}

export function useLastLive(broadcasterId: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.stream.lastLive.queryOptions(
			{ broadcaster_id: broadcasterId },
			{ enabled: !!broadcasterId, staleTime: 60_000 },
		),
	);
}

export function broadcasterLiveOptions(
	trpc: ReturnType<typeof useTRPC>,
	broadcasterId: string,
) {
	return trpc.stream.isLive.queryOptions(
		{ broadcaster_id: broadcasterId },
		{
			staleTime: 30_000,
			refetchOnMount: "always",
			refetchOnWindowFocus: "always",
			refetchInterval: 30_000,
			retry: false,
		},
	);
}

export class LiveStatusReconciler {
	private revision = 0;
	private deltas = new Map<string, { online: boolean; revision: number }>();

	beginSnapshot() {
		return this.revision;
	}

	record(id: string, online: boolean) {
		this.deltas.set(id, { online, revision: ++this.revision });
	}

	reconcile(ids: readonly string[], startedAt: number): string[] {
		const next = new Set(ids);
		for (const [id, delta] of this.deltas) {
			if (delta.revision <= startedAt) this.deltas.delete(id);
			else if (delta.online) next.add(id);
			else next.delete(id);
		}
		return [...next];
	}
}

export function useLiveStreamStatus() {
	const trpc = useTRPC();
	const client = useTRPCClient();
	const qc = useQueryClient();
	const [reconciler] = useState(() => new LiveStatusReconciler());
	const key = trpc.stream.liveIds.queryKey();
	useQuery({
		...trpc.stream.liveIds.queryOptions(undefined, {
			staleTime: Number.POSITIVE_INFINITY,
			refetchOnReconnect: true,
		}),
		queryFn: async ({ signal }) => {
			const startedAt = reconciler.beginSnapshot();
			const ids = await client.stream.liveIds.query(undefined, { signal });
			signal.throwIfAborted();
			return reconciler.reconcile(ids, startedAt);
		},
	});
	useSubscription({
		...trpc.stream.status.subscriptionOptions(),
		onStarted: async () => {
			await Promise.all([
				resyncQuery(qc, trpc.stream.liveIds.pathKey()),
				resyncQuery(qc, trpc.stream.isLive.pathKey()),
				resyncQuery(qc, trpc.video.liveRenditions.pathKey()),
			]);
		},
		onData: (event) => {
			const online = event.kind === "online";
			reconciler.record(event.broadcaster_id, online);
			qc.setQueryData<string[]>(key, (ids) => {
				const next = new Set(ids);
				if (online) next.add(event.broadcaster_id);
				else next.delete(event.broadcaster_id);
				return [...next];
			});
			const input = { broadcaster_id: event.broadcaster_id };
			void resyncQuery(qc, trpc.stream.isLive.queryKey(input));
			void resyncQuery(qc, trpc.video.liveRenditions.queryKey(input));
		},
		onError: withSessionProbe(),
	});
}

const emptyLiveSet = new Set<string>();

export function useLiveSet(): Set<string> {
	const trpc = useTRPC();
	const cache = useQueryClient().getQueryCache();
	const queryHash = hashKey(trpc.stream.liveIds.queryKey());
	const subscribe = useCallback(
		(onChange: () => void) =>
			cache.subscribe((event) => {
				if (event.query.queryHash === queryHash) onChange();
			}),
		[cache, queryHash],
	);
	const getSnapshot = useCallback(
		() => cache.get<string[]>(queryHash)?.state.data,
		[cache, queryHash],
	);
	const ids = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
	return useMemo(() => (ids ? new Set(ids) : emptyLiveSet), [ids]);
}

export function useLiveStreams(max = 5) {
	const trpc = useTRPC();
	const [events, setEvents] = useState<StreamLiveEvent[]>([]);
	const maxRef = useRef(max);
	maxRef.current = max;

	useSubscription({
		...trpc.stream.live.subscriptionOptions(),
		onData: (event) => {
			setEvents((prev) => {
				const next = [event, ...prev];
				return next.length > maxRef.current
					? next.slice(0, maxRef.current)
					: next;
			});
		},
		onError: withSessionProbe(),
	});

	return events;
}
