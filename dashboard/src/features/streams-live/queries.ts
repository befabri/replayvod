import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useRef, useState } from "react";
import type { StreamLiveEvent } from "@/api/generated/trpc";
import { useTRPC, useTRPCClient } from "@/api/trpc";
import { resyncQuery } from "@/lib/query";
import { withSessionProbe } from "@/stores/auth";

// useFollowedStreams fetches currently-live followed channels with
// the full Helix stream shape (title, game, viewer count, thumbnail,
// profile_image_url). Backs the dashboard's "Just went live" card.
export function useFollowedStreams() {
	const trpc = useTRPC();
	return useQuery(
		trpc.stream.followed.queryOptions(undefined, { staleTime: 30_000 }),
	);
}

// useLastLive returns the most recent stream record for a broadcaster:
// the started_at / ended_at pair from the locally-mirrored streams
// table. Reads from the DB, not Twitch — no Helix quota cost — so it's
// safe to fan out per video card. Empty when the broadcaster has no
// recorded streams yet (channel was added but never seen go live).
export function useLastLive(broadcasterId: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.stream.lastLive.queryOptions(
			{ broadcaster_id: broadcasterId },
			{ enabled: !!broadcasterId, staleTime: 60_000 },
		),
	);
}

// Download dialogs verify the selected broadcaster, independently of the
// caller's follows. Poll only while mounted as unscheduled channels may have
// no EventSub subscription; opening or focusing the dialog also rechecks.
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

// Revisions describe local receipt order, independently of clock changes or
// HTTP response timing. Only events received after a snapshot request began
// override that snapshot. One coordinator owns the shared query cache.
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

// Mounted once in the authenticated layout. Snapshot fetches and subscription
// events write the same Query cache, so consumers do not mirror server state
// into component state or open additional subscriptions.
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
			// Omitting force_h264 matches both codec variants for this broadcaster.
			void resyncQuery(qc, trpc.video.liveRenditions.queryKey(input));
		},
		onError: withSessionProbe(),
	});
}

const asLiveSet = (ids: string[]) => new Set(ids);
const emptyLiveSet = new Set<string>();

export function useLiveSet(): Set<string> {
	const trpc = useTRPC();
	// A read-only observer must not replace the coordinator's query function
	// with tRPC's raw snapshot fetcher when Query invalidates this shared key.
	const { data } = useQuery({
		queryKey: trpc.stream.liveIds.queryKey(),
		enabled: false,
		select: asLiveSet,
	});
	return data ?? emptyLiveSet;
}

// useLiveStreams keeps a rolling buffer of the most recent
// stream.live events in React state. These are "we started recording X"
// notifications — distinct from the generic online/offline deltas on
// stream.status. Subscribers render the "Just went live" card from
// this.
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
