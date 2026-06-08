import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useEffect, useRef, useState } from "react";
import type { StreamLiveEvent } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
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

// LiveDelta is one SSE status change, tagged with the wall-clock time it
// arrived so the reseed can tell which deltas raced ahead of a snapshot.
export type LiveDelta = { online: boolean; at: number };

// reconcileLiveSet rebuilds the live set from an authoritative snapshot, then
// layers on only the deltas that arrived AFTER the snapshot (snapshotAt). Deltas
// at or before the snapshot are superseded by it and pruned from `deltas` (the
// one mutation), so a stale online/offline event can never be replayed over a
// newer bootstrap. Extracted as a pure function so the reconciliation rules are
// unit tested directly.
export function reconcileLiveSet(
	ids: readonly string[],
	snapshotAt: number,
	deltas: Map<string, LiveDelta>,
): Set<string> {
	const next = new Set(ids);
	for (const [broadcasterId, delta] of deltas) {
		if (delta.at <= snapshotAt) {
			deltas.delete(broadcasterId);
		} else if (delta.online) {
			next.add(broadcasterId);
		} else {
			next.delete(broadcasterId);
		}
	}
	return next;
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
	const { data: ids, dataUpdatedAt } = useQuery(
		trpc.stream.liveIds.queryOptions(undefined, {
			staleTime: Number.POSITIVE_INFINITY,
			refetchOnReconnect: true,
		}),
	);

	const [liveSet, setLiveSet] = useState<Set<string>>(() => new Set());

	// SSE deltas tagged with the wall-clock time they arrived. The reseed below
	// replaces the Set wholesale from the Helix snapshot, so channels that went
	// offline during a disconnect are dropped. A delta can land after the
	// refetch resolves yet before the reseed effect runs, so re-applying deltas
	// that arrived AFTER the snapshot keeps that just-received change instead of
	// losing it. Deltas older than the snapshot are superseded by it and pruned,
	// so a stale event can't be replayed over a newer authoritative bootstrap.
	const pendingDeltas = useRef<Map<string, LiveDelta>>(new Map());

	const applyDelta = (broadcasterId: string, online: boolean) => {
		pendingDeltas.current.set(broadcasterId, { online, at: Date.now() });
		setLiveSet((prev) => {
			const next = new Set(prev);
			if (online) next.add(broadcasterId);
			else next.delete(broadcasterId);
			return next;
		});
	};

	// Reseed from the bootstrap snapshot, layering on only the deltas that raced
	// ahead of it. reconcileLiveSet prunes anything the snapshot supersedes, so a
	// stale delta can't be replayed over a newer authoritative bootstrap.
	useEffect(() => {
		if (!ids) return;
		setLiveSet(reconcileLiveSet(ids, dataUpdatedAt, pendingDeltas.current));
	}, [ids, dataUpdatedAt]);

	useSubscription({
		...trpc.stream.status.subscriptionOptions(),
		onData: (ev) => {
			applyDelta(ev.broadcaster_id, ev.kind === "online");
		},
		// Force a fresh Helix bootstrap whenever the SSE socket errors. Closes
		// the reconnect-window gap where deltas fired during the disconnect are
		// lost. Without this, staleTime: Infinity would keep us on a stale Set
		// until the next navigator-level network reconnect. The reseed's
		// timestamp prune above discards any deltas the fresh snapshot
		// supersedes, so there's nothing to clear here.
		//
		// No withSessionProbe here (unlike the other live feeds): this
		// invalidation refetches the authenticated liveIds bootstrap, so an
		// expired session 401s through the shared cache interceptor on its own.
		// Adding a probe would just double the request on every reconnect.
		onError: () => {
			qc.invalidateQueries({ queryKey: trpc.stream.liveIds.pathKey() });
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
		onError: withSessionProbe(),
	});

	return events;
}
