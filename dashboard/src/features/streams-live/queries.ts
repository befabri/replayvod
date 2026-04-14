import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useEffect, useRef, useState } from "react";
import type { StreamLiveEvent } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";

// useFollowedStreams fetches currently-live followed channels with
// the full Helix stream shape (title, game, viewer count, thumbnail,
// profile_image_url). Backs the dashboard's "Just went live" card.
export function useFollowedStreams() {
	const trpc = useTRPC();
	return useQuery(
		trpc.stream.followed.queryOptions(undefined, { staleTime: 30_000 }),
	);
}

// useLiveSet maintains a live `Set<broadcaster_id>` of currently-live
// followed channels. Bootstraps from `stream.liveIds` (one Helix-backed
// query) and then stays in sync via the `stream.status` SSE feed — no
// background polling after the initial call. The Helix snapshot is a
// bootstrapping payload + reconnect safety net; SSE deltas are the
// source of truth once the socket is live.
//
// Reconnect contract:
//   - navigator.onLine reconnect → refetchOnReconnect refires the
//     bootstrap query automatically. Covers network drops.
//   - SSE-only disconnect (backend restart, idle timeout, proxy
//     reset) does NOT flip navigator.onLine, so onError explicitly
//     invalidates the bootstrap query on every SSE error. The
//     subscription auto-reconnects; the bootstrap refetches with
//     fresh data; the Set is rebuilt from scratch. Without this
//     the Set would drift silently past any SSE-layer drop.
//
// Backend authoritativeness: this Set is only as correct as the
// EventSub subs backing stream.status. The scheduler's
// eventsub_reconcile_channels task + boot reconcile ensure every
// channel has both stream.online and stream.offline subs so the
// delta feed is complete.
export function useLiveSet(): Set<string> {
	const trpc = useTRPC();
	const qc = useQueryClient();
	const { data: ids } = useQuery(
		trpc.stream.liveIds.queryOptions(undefined, {
			staleTime: Number.POSITIVE_INFINITY,
			refetchOnReconnect: true,
		}),
	);

	const [liveSet, setLiveSet] = useState<Set<string>>(() => new Set());

	// Seed the set from the bootstrap payload. Each refetch (on
	// reconnect or manual invalidation) replaces the set wholesale
	// so missed SSE events are reconciled.
	useEffect(() => {
		if (ids) setLiveSet(new Set(ids));
	}, [ids]);

	useSubscription({
		...trpc.stream.status.subscriptionOptions(),
		onData: (ev) => {
			setLiveSet((prev) => {
				const next = new Set(prev);
				if (ev.kind === "online") next.add(ev.broadcaster_id);
				else next.delete(ev.broadcaster_id);
				return next;
			});
		},
		// Force a fresh Helix bootstrap whenever the SSE socket
		// errors. Closes the reconnect-window gap where deltas
		// fired during the disconnect are lost — without this,
		// staleTime: Infinity would keep us on a stale Set until
		// the next navigator-level network reconnect.
		onError: () => {
			qc.invalidateQueries({ queryKey: trpc.stream.liveIds.queryKey() });
		},
	});

	return liveSet;
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
	});

	return events;
}
