import { useSelector } from "@tanstack/react-store";
import { useEffect, useState } from "react";
import type { VideoUserStateResponse } from "@/api/generated/trpc";
import { authStore } from "@/stores/auth";
import {
	clearLocalWatchProgress,
	type LocalWatchProgress,
	readLocalWatchProgress,
} from "./watch-progress";

// A saved position resumes only when it is worth it: at least this far in,
// and not so close to the end that the viewer clearly finished. The end margin
// scales down for short clips so a minute-long clip still resumes near its end.
export const RESUME_MIN_SECONDS = 5;
export const RESUME_END_MARGIN_SECONDS = 30;
export const RESUME_END_MARGIN_FRACTION = 0.05;

// resumeOffsetSeconds picks the offset the player opens at for a saved watch
// position, or undefined to start from the beginning. Only the position
// counts: completed_at records that the recording was once watched through,
// and a rewatch that stopped halfway still resumes.
export function resumeOffsetSeconds(
	state:
		| Pick<VideoUserStateResponse, "last_position_seconds">
		| null
		| undefined,
	totalDurationSeconds: number,
): number | undefined {
	const position = state?.last_position_seconds;
	if (position == null || !Number.isFinite(position)) return undefined;
	if (position < RESUME_MIN_SECONDS) return undefined;
	if (Number.isFinite(totalDurationSeconds) && totalDurationSeconds > 0) {
		const margin = Math.min(
			RESUME_END_MARGIN_SECONDS,
			totalDurationSeconds * RESUME_END_MARGIN_FRACTION,
		);
		if (position >= totalDurationSeconds - margin) return undefined;
	}
	return position;
}

export type ResumeSeed = {
	// Where the player opens; undefined starts from the beginning.
	offsetSeconds: number | undefined;
	// A local write the server never confirmed and that is newer than what
	// it holds; the page sends it again.
	replay: LocalWatchProgress | null;
};

// resolveResume merges the server's saved position with the local mirror. The
// mirror wins only when it is newer than the server's state and says
// something different; otherwise it is stale and gets dropped.
export function resolveResume({
	server,
	local,
	totalDurationSeconds,
}: {
	server: VideoUserStateResponse | null | undefined;
	local: LocalWatchProgress | null;
	totalDurationSeconds: number;
}): ResumeSeed {
	// The mirror records the server revision it was based on. Browser wall
	// clocks are irrelevant: any later server write supersedes that baseline.
	const localIsNewer =
		local != null &&
		local.baseProgressRevision === (server?.progress_revision ?? 0) &&
		(server == null ||
			local.positionSeconds !== server.last_position_seconds ||
			(local.completed && !server.completed_at));
	if (localIsNewer) {
		return {
			offsetSeconds: resumeOffsetSeconds(
				{ last_position_seconds: local.positionSeconds },
				totalDurationSeconds,
			),
			replay: local,
		};
	}
	return {
		offsetSeconds: resumeOffsetSeconds(server, totalDurationSeconds),
		replay: null,
	};
}

type LatchedSeed = {
	videoId: number;
	userId: string | null;
	seed: ResumeSeed;
	staleLocal: LocalWatchProgress | null;
};

// useResume seeds the player once per recording. Every progress write patches
// the saved position in the video cache, and the player treats a new initial
// offset as a new seek, so the seed must not follow the live value while the
// viewer is watching. A stale local mirror is cleared only after this seed
// commits, and only if the mirror has not changed since it was read.
export function useResume(
	video:
		| { id: number; user_state?: VideoUserStateResponse | null }
		| null
		| undefined,
	totalDurationSeconds: number,
): ResumeSeed {
	const userId = useSelector(authStore, (state) => state.user?.id ?? null);
	const [latched, setLatched] = useState<LatchedSeed | null>(null);
	useEffect(() => {
		if (latched?.userId && latched.staleLocal) {
			clearLocalWatchProgress(
				latched.userId,
				latched.videoId,
				latched.staleLocal,
			);
		}
	}, [latched]);

	if (!video) return { offsetSeconds: undefined, replay: null };
	if (latched?.videoId === video.id && latched.userId === userId) {
		return latched.seed;
	}
	const local = userId ? readLocalWatchProgress(userId, video.id) : null;
	const seed = resolveResume({
		server: video.user_state,
		local,
		totalDurationSeconds,
	});
	setLatched({
		videoId: video.id,
		userId,
		seed,
		staleLocal: seed.replay ? null : local,
	});
	return seed;
}
