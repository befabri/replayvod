// @vitest-environment jsdom

import {
	act,
	cleanup,
	render,
	renderHook,
	screen,
} from "@testing-library/react";
import { StrictMode, Suspense, useLayoutEffect } from "react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { VideoUserStateResponse } from "@/api/generated/trpc";
import { type AuthUser, authStore } from "@/stores/auth";
import { installMemoryStorage } from "@/test/memory-storage";
import { resolveResume, resumeOffsetSeconds, useResume } from "./resume";
import {
	type LocalWatchProgress,
	localWatchProgressKey,
	readLocalWatchProgress,
	writeLocalWatchProgress,
} from "./watch-progress";

afterEach(cleanup);

function state(
	last_position_seconds: number,
	extra: Partial<VideoUserStateResponse> = {},
): VideoUserStateResponse {
	return {
		watch_later: false,
		progress_revision: 10,
		last_position_seconds,
		updated_at: "2026-01-02T00:00:00Z",
		...extra,
	};
}

const serverUpdatedMs = Date.parse("2026-01-02T00:00:00Z");

function local(
	positionSeconds: number,
	savedAtMs: number,
	completed = false,
): LocalWatchProgress {
	return {
		positionSeconds,
		completed,
		savedAtMs,
		baseProgressRevision: savedAtMs < serverUpdatedMs ? 9 : 10,
	};
}

describe("resumeOffsetSeconds", () => {
	it.each<
		[string, VideoUserStateResponse | undefined, number, number | undefined]
	>([
		["no saved state", undefined, 3600, undefined],
		["never played", state(0), 3600, undefined],
		["under the minimum", state(4.9), 3600, undefined],
		["at the minimum", state(5), 3600, 5],
		["midway", state(1234.5), 3600, 1234.5],
		["inside the end margin of a long recording", state(3571), 3600, undefined],
		["just before the end margin of a long recording", state(3569), 3600, 3569],
		["inside the scaled margin of a short clip", state(57.5), 60, undefined],
		["before the scaled margin of a short clip", state(56), 60, 56],
		["past the end", state(4000), 3600, undefined],
		["unknown duration keeps only the minimum rule", state(5000), 0, 5000],
		["non-finite position", state(Number.NaN), 3600, undefined],
		[
			"non-finite duration keeps only the minimum rule",
			state(7),
			Number.NaN,
			7,
		],
	])("%s", (_, saved, total, expected) => {
		expect(resumeOffsetSeconds(saved, total)).toBe(expected);
	});

	it("ignores completed_at so a rewatch that stopped halfway resumes", () => {
		const rewatch = state(900, { completed_at: "2026-01-01T00:00:00Z" });
		expect(resumeOffsetSeconds(rewatch, 3600)).toBe(900);
	});

	it("starts a finished recording over even though completed_at is set", () => {
		const finished = state(3600, { completed_at: "2026-01-01T00:00:00Z" });
		expect(resumeOffsetSeconds(finished, 3600)).toBeUndefined();
	});
});

describe("resolveResume", () => {
	it.each([
		-86_400_000, 86_400_000,
	])("uses the server revision with browser clock skew %s", (skew) => {
		const pending = {
			...local(135, serverUpdatedMs + skew),
			baseProgressRevision: 10,
		};
		expect(
			resolveResume({
				server: state(120),
				local: pending,
				totalDurationSeconds: 3600,
			}).replay,
		).toEqual(pending);
		expect(
			resolveResume({
				server: state(140, { progress_revision: 11 }),
				local: pending,
				totalDurationSeconds: 3600,
			}),
		).toEqual({ offsetSeconds: 140, replay: null });
	});

	it("replays completion even when the position already matches", () => {
		const pending = local(120, serverUpdatedMs, true);
		expect(
			resolveResume({
				server: state(120),
				local: pending,
				totalDurationSeconds: 3600,
			}).replay,
		).toEqual(pending);
	});

	it("uses the server position without a local mirror", () => {
		expect(
			resolveResume({
				server: state(120),
				local: null,
				totalDurationSeconds: 3600,
			}),
		).toEqual({ offsetSeconds: 120, replay: null });
	});

	it("prefers a newer unconfirmed local write and hands it back for replay", () => {
		const unconfirmed = local(135, serverUpdatedMs + 15_000);
		expect(
			resolveResume({
				server: state(120),
				local: unconfirmed,
				totalDurationSeconds: 3600,
			}),
		).toEqual({ offsetSeconds: 135, replay: unconfirmed });
	});

	it("seeds from the local mirror when the server has nothing yet", () => {
		const unconfirmed = {
			...local(40, serverUpdatedMs),
			baseProgressRevision: 0,
		};
		expect(
			resolveResume({
				server: undefined,
				local: unconfirmed,
				totalDurationSeconds: 3600,
			}),
		).toEqual({ offsetSeconds: 40, replay: unconfirmed });
	});

	it("drops a local mirror older than the server state", () => {
		expect(
			resolveResume({
				server: state(120),
				local: local(90, serverUpdatedMs - 1),
				totalDurationSeconds: 3600,
			}),
		).toEqual({ offsetSeconds: 120, replay: null });
	});

	it("drops a local mirror the server already holds", () => {
		expect(
			resolveResume({
				server: state(120),
				local: local(120, serverUpdatedMs + 1),
				totalDurationSeconds: 3600,
			}),
		).toEqual({ offsetSeconds: 120, replay: null });
	});

	it("treats a newer completed local write as finished but still replays it", () => {
		const finished = local(3600, serverUpdatedMs + 1, true);
		expect(
			resolveResume({
				server: state(3000),
				local: finished,
				totalDurationSeconds: 3600,
			}),
		).toEqual({ offsetSeconds: undefined, replay: finished });
	});
});

describe("useResume", () => {
	type Video = { id: number; user_state?: VideoUserStateResponse };
	const user = { id: "u1", role: "viewer" } as unknown as AuthUser;

	beforeEach(() => {
		installMemoryStorage();
		authStore.setState((current) => ({
			...current,
			isAuthenticated: true,
			isLoading: false,
			user,
		}));
	});

	it("latches the seed per recording and ignores later progress writes", () => {
		const { result, rerender } = renderHook(
			({ video, total }: { video: Video | undefined; total: number }) =>
				useResume(video, total),
			{ initialProps: { video: undefined as Video | undefined, total: 0 } },
		);
		expect(result.current.offsetSeconds).toBeUndefined();

		rerender({ video: { id: 1, user_state: state(120) }, total: 3600 });
		expect(result.current.offsetSeconds).toBe(120);

		// The player saved progress and the cache patched the video: same seed.
		rerender({ video: { id: 1, user_state: state(135) }, total: 3600 });
		expect(result.current.offsetSeconds).toBe(120);
		rerender({ video: { id: 1, user_state: state(3599) }, total: 3600 });
		expect(result.current.offsetSeconds).toBe(120);

		// Another recording seeds afresh.
		rerender({ video: { id: 2, user_state: state(40) }, total: 3600 });
		expect(result.current.offsetSeconds).toBe(40);
		rerender({ video: { id: 2 }, total: 3600 });
		expect(result.current.offsetSeconds).toBe(40);
	});

	it("seeds from a newer local mirror and hands it back for replay", () => {
		const unconfirmed = local(150, serverUpdatedMs + 60_000);
		writeLocalWatchProgress("u1", 3, unconfirmed);
		const { result } = renderHook(() =>
			useResume({ id: 3, user_state: state(120) }, 3600),
		);
		expect(result.current).toEqual({ offsetSeconds: 150, replay: unconfirmed });
		// The mirror stays until the replayed write is confirmed.
		expect(readLocalWatchProgress("u1", 3)).toEqual(unconfirmed);
	});

	it("clears a stale local mirror", () => {
		writeLocalWatchProgress("u1", 4, local(90, serverUpdatedMs - 60_000));
		const { result } = renderHook(() =>
			useResume({ id: 4, user_state: state(120) }, 3600),
		);
		expect(result.current).toEqual({ offsetSeconds: 120, replay: null });
		expect(
			window.localStorage.getItem(localWatchProgressKey("u1", 4)),
		).toBeNull();
	});

	it("clears stale progress only after the render commits", () => {
		const stale = local(90, serverUpdatedMs - 60_000);
		writeLocalWatchProgress("u1", 4, stale);
		let atCommit: LocalWatchProgress | null = null;
		const { result } = renderHook(() => {
			const seed = useResume({ id: 4, user_state: state(120) }, 3600);
			useLayoutEffect(() => {
				atCommit = readLocalWatchProgress("u1", 4);
			}, []);
			return seed;
		});
		expect(atCommit).toEqual(stale);
		expect(result.current).toEqual({ offsetSeconds: 120, replay: null });
		expect(readLocalWatchProgress("u1", 4)).toBeNull();
	});

	it("preserves the mirror when a resume render suspends and is abandoned", () => {
		const stale = local(90, serverUpdatedMs - 60_000);
		writeLocalWatchProgress("u1", 4, stale);
		const pending = new Promise<never>(() => {});
		function SuspendedResume(): never {
			useResume({ id: 4, user_state: state(120) }, 3600);
			throw pending;
		}
		const { unmount } = render(
			<StrictMode>
				<Suspense fallback={<span>Loading recording</span>}>
					<SuspendedResume />
				</Suspense>
			</StrictMode>,
		);
		expect(screen.getByText("Loading recording")).toBeTruthy();
		expect(readLocalWatchProgress("u1", 4)).toEqual(stale);
		unmount();
		expect(readLocalWatchProgress("u1", 4)).toEqual(stale);
	});

	it.each([
		["position", local(95, serverUpdatedMs - 60_000)],
		["timestamp", local(90, serverUpdatedMs + 1)],
		["completion", local(90, serverUpdatedMs - 60_000, true)],
	])("preserves a mirror whose %s changed before cleanup", (_field, newer) => {
		writeLocalWatchProgress("u1", 4, local(90, serverUpdatedMs - 60_000));
		renderHook(
			() => {
				const seed = useResume({ id: 4, user_state: state(120) }, 3600);
				useLayoutEffect(() => {
					writeLocalWatchProgress("u1", 4, newer);
				}, []);
				return seed;
			},
			{ wrapper: StrictMode },
		);
		expect(readLocalWatchProgress("u1", 4)).toEqual(newer);
	});

	it("reseeds for a different user watching the same recording", () => {
		writeLocalWatchProgress("u1", 5, local(150, serverUpdatedMs + 1));
		writeLocalWatchProgress("u2", 5, local(180, serverUpdatedMs + 1));
		const { result } = renderHook(() =>
			useResume({ id: 5, user_state: state(120) }, 3600),
		);
		expect(result.current.offsetSeconds).toBe(150);
		act(() => {
			authStore.setState((current) => ({
				...current,
				user: { ...user, id: "u2" },
			}));
		});
		expect(result.current.offsetSeconds).toBe(180);
		expect(result.current.replay).toEqual(local(180, serverUpdatedMs + 1));
	});

	it("ignores the mirror of another user", () => {
		writeLocalWatchProgress("someone-else", 5, local(150, serverUpdatedMs + 1));
		const { result } = renderHook(() =>
			useResume({ id: 5, user_state: state(120) }, 3600),
		);
		expect(result.current).toEqual({ offsetSeconds: 120, replay: null });
	});
});
