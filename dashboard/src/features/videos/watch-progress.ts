import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSelector } from "@tanstack/react-store";
import { useCallback } from "react";
import { KEEPALIVE_CONTEXT } from "@/api/links";
import { useTRPC, useTRPCClient } from "@/api/trpc";
import { invalidateCaches, patchEntity } from "@/lib/query";
import { authStore } from "@/stores/auth";
import {
	VIDEO_USER_STATE_CACHES,
	videoCaches,
	videoUserStatePatch,
} from "./cache";

// LocalWatchProgress is the copy of a progress write kept in localStorage
// until the server confirms it. Whatever is still there when the recording is
// opened again was never confirmed: a flush the server missed while it was
// restarting, or a tab closed before the response arrived.
export type LocalWatchProgress = {
	positionSeconds: number;
	completed: boolean;
	savedAtMs: number;
	baseProgressRevision: number;
};

const STORAGE_PREFIX = "replayvod:watch-progress:";

export function localWatchProgressKey(userId: string, videoId: number) {
	return `${STORAGE_PREFIX}${userId}:${videoId}`;
}

export function readLocalWatchProgress(
	userId: string,
	videoId: number,
): LocalWatchProgress | null {
	try {
		const raw = window.localStorage.getItem(
			localWatchProgressKey(userId, videoId),
		);
		if (raw == null) return null;
		const parsed: unknown = JSON.parse(raw);
		if (!parsed || typeof parsed !== "object") return null;
		const { positionSeconds, completed, savedAtMs, baseProgressRevision } =
			parsed as Partial<LocalWatchProgress>;
		if (
			typeof positionSeconds !== "number" ||
			!Number.isFinite(positionSeconds) ||
			positionSeconds < 0 ||
			typeof savedAtMs !== "number" ||
			!Number.isFinite(savedAtMs) ||
			typeof baseProgressRevision !== "number" ||
			!Number.isFinite(baseProgressRevision) ||
			baseProgressRevision < 0
		) {
			return null;
		}
		return {
			positionSeconds,
			completed: completed === true,
			savedAtMs,
			baseProgressRevision,
		};
	} catch {
		return null;
	}
}

export function writeLocalWatchProgress(
	userId: string,
	videoId: number,
	entry: LocalWatchProgress,
) {
	try {
		window.localStorage.setItem(
			localWatchProgressKey(userId, videoId),
			JSON.stringify(entry),
		);
	} catch {
		// Storage unavailable (private mode, quota): the server write still goes out.
	}
}

// clearLocalWatchProgress drops the mirror only if it still matches every
// supplied field. A server acknowledgement matches the confirmed position;
// deferred resume cleanup supplies the entire snapshot so it cannot erase a
// replacement written since render, even at the same position.
export function clearLocalWatchProgress(
	userId: string,
	videoId: number,
	expected?: Partial<LocalWatchProgress>,
) {
	try {
		if (expected) {
			const current = readLocalWatchProgress(userId, videoId);
			if (
				!current ||
				(
					[
						"positionSeconds",
						"completed",
						"savedAtMs",
						"baseProgressRevision",
					] as const
				).some(
					(field) =>
						expected[field] !== undefined && current[field] !== expected[field],
				)
			) {
				return;
			}
		}
		window.localStorage.removeItem(localWatchProgressKey(userId, videoId));
	} catch {
		// Nothing to clear when storage is unavailable.
	}
}

// useWatchProgressWriter returns the one function the watch page saves
// progress through. Every write is mirrored locally first and sent on a
// keepalive request, so a flush from pagehide lands after the tab is gone;
// the server response patches the recording wherever it is cached and drops
// the mirror.
export function useWatchProgressWriter(videoId: number) {
	const userId = useSelector(authStore, (state) => state.user?.id ?? null);
	const trpc = useTRPC();
	const client = useTRPCClient();
	const queryClient = useQueryClient();
	const caches = videoCaches(trpc);
	const { mutate } = useMutation({
		mutationFn: ({
			entry,
			videoId,
		}: {
			entry: LocalWatchProgress;
			ownerId: string;
			videoId: number;
		}) =>
			client.video.updateWatchProgress.mutate(
				{
					video_id: videoId,
					position_seconds: entry.positionSeconds,
					completed: entry.completed,
				},
				{ context: KEEPALIVE_CONTEXT },
			),
		onSuccess: (state, { entry, ownerId, videoId }) => {
			clearLocalWatchProgress(ownerId, videoId, {
				positionSeconds: entry.positionSeconds,
				completed: entry.completed,
				savedAtMs: entry.savedAtMs,
			});
			// A response belonging to a previous login must never patch the
			// current user's cache. Late responses must not rewind it either.
			if (authStore.state.user?.id !== ownerId) return;
			const key = trpc.video.getById.queryKey({ id: videoId });
			const current = queryClient.getQueryData(key)?.user_state;
			if ((current?.progress_revision ?? 0) > (state.progress_revision ?? 0))
				return;
			// An earlier write can be acknowledged while a newer local write is
			// still pending. Advance that mirror's baseline only for our own
			// accepted write, so its retry survives this acknowledgement.
			const pending = readLocalWatchProgress(ownerId, videoId);
			if (
				pending &&
				pending.baseProgressRevision >= entry.baseProgressRevision &&
				pending.baseProgressRevision <= (state.progress_revision ?? 0) &&
				state.last_position_seconds === entry.positionSeconds &&
				(state.progress_revision ?? 0) > entry.baseProgressRevision
			) {
				writeLocalWatchProgress(ownerId, videoId, {
					...pending,
					baseProgressRevision: state.progress_revision ?? 0,
				});
			}
			patchEntity(queryClient, caches, videoUserStatePatch(videoId, state));
			invalidateCaches(queryClient, caches, VIDEO_USER_STATE_CACHES);
		},
	});
	return useCallback(
		(positionSeconds: number, completed: boolean) => {
			if (!Number.isFinite(positionSeconds) || videoId <= 0) return;
			const position = Math.max(0, positionSeconds);
			if (!userId || authStore.state.user?.id !== userId) return;
			const current = queryClient.getQueryData(
				trpc.video.getById.queryKey({ id: videoId }),
			)?.user_state;
			const entry: LocalWatchProgress = {
				positionSeconds: position,
				completed,
				savedAtMs: Date.now(),
				baseProgressRevision: current?.progress_revision ?? 0,
			};
			writeLocalWatchProgress(userId, videoId, entry);
			mutate({ entry, ownerId: userId, videoId });
		},
		[mutate, userId, videoId, queryClient, trpc],
	);
}
