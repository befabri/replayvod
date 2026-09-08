// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink, type TRPCLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { type AuthUser, authStore } from "@/stores/auth";
import { installMemoryStorage } from "@/test/memory-storage";
import { useVideo } from "./queries";
import {
	clearLocalWatchProgress,
	localWatchProgressKey,
	readLocalWatchProgress,
	useWatchProgressWriter,
	writeLocalWatchProgress,
} from "./watch-progress";

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
});

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

describe("local watch progress mirror", () => {
	it("round-trips an entry per user and recording", () => {
		const entry = {
			positionSeconds: 42.5,
			completed: false,
			savedAtMs: 1_000,
			baseProgressRevision: 0,
		};
		writeLocalWatchProgress("u1", 7, entry);
		expect(readLocalWatchProgress("u1", 7)).toEqual(entry);
		expect(readLocalWatchProgress("u2", 7)).toBeNull();
		expect(readLocalWatchProgress("u1", 8)).toBeNull();
	});

	it("rejects entries that are not a progress record", () => {
		for (const raw of [
			"garbage",
			"null",
			"[]",
			JSON.stringify({
				positionSeconds: "12",
				savedAtMs: 1,
				baseProgressRevision: 0,
			}),
			JSON.stringify({
				positionSeconds: -1,
				savedAtMs: 1,
				baseProgressRevision: 0,
			}),
			JSON.stringify({ positionSeconds: 12 }),
		]) {
			window.localStorage.setItem(localWatchProgressKey("u1", 7), raw);
			expect(readLocalWatchProgress("u1", 7)).toBeNull();
		}
	});

	it("clears only the position that was confirmed", () => {
		writeLocalWatchProgress("u1", 7, {
			positionSeconds: 60,
			completed: false,
			savedAtMs: 2,
			baseProgressRevision: 0,
		});
		// An older write's late success must not drop the newer mirror.
		clearLocalWatchProgress("u1", 7, { positionSeconds: 45 });
		expect(readLocalWatchProgress("u1", 7)?.positionSeconds).toBe(60);
		clearLocalWatchProgress("u1", 7, { positionSeconds: 60 });
		expect(readLocalWatchProgress("u1", 7)).toBeNull();

		writeLocalWatchProgress("u1", 7, {
			positionSeconds: 60,
			completed: false,
			savedAtMs: 3,
			baseProgressRevision: 0,
		});
		clearLocalWatchProgress("u1", 7);
		expect(readLocalWatchProgress("u1", 7)).toBeNull();
	});
});

function json(data: unknown) {
	return new Response(JSON.stringify({ result: { data } }), {
		headers: { "Content-Type": "application/json" },
	});
}

// harness wires the hooks to a fake transport and records the op context each
// procedure was sent with.
function harness(
	fetch: (url: string, options?: RequestInit) => Promise<Response>,
	contexts: Record<string, unknown>[],
) {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: { retry: false, staleTime: Infinity, gcTime: Infinity },
			mutations: { retry: false },
		},
	});
	const captureContext: TRPCLink<AppRouter> =
		() =>
		({ op, next }) => {
			contexts.push({ path: op.path, ...op.context });
			return next(op);
		};
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			captureContext,
			httpLink({
				url: "http://example.test/trpc",
				fetch: (url, options) => fetch(String(url), options),
			}),
		],
	});
	const wrapper = ({ children }: { children: ReactNode }) =>
		createElement(
			QueryClientProvider,
			{ client: queryClient },
			createElement(TRPCProvider, { trpcClient, queryClient, children }),
		);
	return { wrapper, queryClient };
}

const recording = {
	id: 7,
	status: "DONE",
	is_audio_only: false,
	parts: [{ part_index: 1 }],
	playback_artifact: { status: "ready" },
	user_state: {
		watch_later: true,
		last_position_seconds: 0,
		updated_at: "2026-01-01T00:00:00Z",
	},
};

const listPageKey = [["video", "listPage"], { input: {}, type: "infinite" }];
const statsKey = [
	["video", "statisticsByBroadcaster"],
	{ input: { broadcaster_id: "chan1" }, type: "query" },
];

describe("useWatchProgressWriter", () => {
	it("does not let a late acknowledgement erase newer completion or rewind the cache", async () => {
		const pending: Array<(value: Response) => void> = [];
		const { wrapper, queryClient } = harness(
			() => new Promise<Response>((resolve) => pending.push(resolve)),
			[],
		);
		const key = [["video", "getById"], { input: { id: 7 }, type: "query" }];
		queryClient.setQueryData(key, recording);
		const { result } = renderHook(() => useWatchProgressWriter(7), { wrapper });
		vi.spyOn(Date, "now").mockReturnValue(1000);
		act(() => result.current(42, false));
		await waitFor(() => expect(pending).toHaveLength(1));
		act(() => result.current(42, true));
		await waitFor(() => expect(pending).toHaveLength(2));
		// Second response arrives first, then a new unsaved position is mirrored.
		pending[1](
			json({
				...recording.user_state,
				last_position_seconds: 42,
				completed_at: "2026-01-01T00:00:01Z",
				progress_revision: 2,
			}),
		);
		await waitFor(() => expect(readLocalWatchProgress("u1", 7)).toBeNull());
		act(() => result.current(60, false));
		await waitFor(() => expect(pending).toHaveLength(3));
		pending[0](
			json({
				...recording.user_state,
				last_position_seconds: 42,
				progress_revision: 1,
			}),
		);
		await waitFor(() =>
			expect(
				queryClient
					.getMutationCache()
					.getAll()
					.filter((m) => m.state.status === "success"),
			).toHaveLength(2),
		);
		expect(readLocalWatchProgress("u1", 7)?.positionSeconds).toBe(60);
		expect(queryClient.getQueryData(key)).toMatchObject({
			user_state: {
				progress_revision: 2,
				completed_at: "2026-01-01T00:00:01Z",
			},
		});
	});

	it("rebases a pending local write on its own earlier acknowledgement", async () => {
		const pending: Array<(value: Response) => void> = [];
		const { wrapper, queryClient } = harness(
			() => new Promise<Response>((resolve) => pending.push(resolve)),
			[],
		);
		const key = [["video", "getById"], { input: { id: 7 }, type: "query" }];
		queryClient.setQueryData(key, recording);
		const { result } = renderHook(() => useWatchProgressWriter(7), { wrapper });
		act(() => result.current(42, false));
		await waitFor(() => expect(pending).toHaveLength(1));
		act(() => result.current(60, false));
		await waitFor(() => expect(pending).toHaveLength(2));
		pending[0](
			json({
				...recording.user_state,
				last_position_seconds: 42,
				progress_revision: 1,
			}),
		);
		await waitFor(() =>
			expect(readLocalWatchProgress("u1", 7)?.baseProgressRevision).toBe(1),
		);
		expect(readLocalWatchProgress("u1", 7)?.positionSeconds).toBe(60);
		pending[1](
			json({
				...recording.user_state,
				last_position_seconds: 60,
				progress_revision: 2,
			}),
		);
		await waitFor(() => expect(readLocalWatchProgress("u1", 7)).toBeNull());
	});

	it("keeps acknowledgements scoped to the user who sent the write", async () => {
		let respond: ((response: Response) => void) | undefined;
		const { wrapper, queryClient } = harness(
			() =>
				new Promise<Response>((resolve) => {
					respond = resolve;
				}),
			[],
		);
		const key = [["video", "getById"], { input: { id: 7 }, type: "query" }];
		queryClient.setQueryData(key, recording);
		const { result } = renderHook(() => useWatchProgressWriter(7), { wrapper });
		act(() => result.current(42, false));
		await waitFor(() => expect(respond).toBeDefined());
		act(() =>
			authStore.setState((state) => ({
				...state,
				user: { ...user, id: "u2" },
			})),
		);
		writeLocalWatchProgress("u2", 7, {
			positionSeconds: 42,
			completed: false,
			savedAtMs: 1,
			baseProgressRevision: 0,
		});
		respond?.(
			json({
				...recording.user_state,
				last_position_seconds: 42,
				progress_revision: 1,
			}),
		);
		await waitFor(() => expect(readLocalWatchProgress("u1", 7)).toBeNull());
		expect(readLocalWatchProgress("u2", 7)).not.toBeNull();
		expect(queryClient.getQueryData(key)).toEqual(recording);
	});

	it.each([
		0, -1,
	])("rejects invalid video id %s without a request", async (id) => {
		const fetch = vi.fn(async () => json(recording));
		const { wrapper } = harness(fetch, []);
		const { result } = renderHook(() => useWatchProgressWriter(id), {
			wrapper,
		});
		await act(async () => result.current(42, false));
		expect(fetch).not.toHaveBeenCalled();
	});

	it.each([
		Number.NaN,
		Number.POSITIVE_INFINITY,
		Number.NEGATIVE_INFINITY,
	])("rejects non-finite progress %s without a request", async (position) => {
		const fetch = vi.fn(async () => json(recording));
		const { wrapper } = harness(fetch, []);
		const { result } = renderHook(() => useWatchProgressWriter(7), { wrapper });
		await act(async () => result.current(position, false));
		expect(fetch).not.toHaveBeenCalled();
		expect(readLocalWatchProgress("u1", 7)).toBeNull();
	});

	it("mirrors the write, sends it on a keepalive request, patches the recording and refreshes only the lists", async () => {
		const contexts: Record<string, unknown>[] = [];
		const bodies: unknown[] = [];
		const { wrapper, queryClient } = harness(async (url, options) => {
			const procedure = new URL(url).pathname;
			if (options?.method === "POST") {
				expect(procedure).toBe("/trpc/video.updateWatchProgress");
				const input = JSON.parse(String(options.body));
				bodies.push(input);
				return json({
					watch_later: false,
					last_position_seconds: input.position_seconds,
					watched_at: "2026-01-01T00:00:00Z",
					updated_at: "2026-01-01T00:00:01Z",
				});
			}
			expect(procedure).toBe("/trpc/video.getById");
			return json(recording);
		}, contexts);
		queryClient.setQueryData(listPageKey, { pages: [], pageParams: [] });
		queryClient.setQueryData(statsKey, { total: 1 });

		const { result } = renderHook(
			() => ({ video: useVideo(7), write: useWatchProgressWriter(7) }),
			{ wrapper },
		);
		await waitFor(() => expect(result.current.video.data?.id).toBe(7));

		act(() => {
			result.current.write(42, false);
		});
		// Mirrored before the network answers.
		expect(readLocalWatchProgress("u1", 7)?.positionSeconds).toBe(42);

		await waitFor(() =>
			expect(result.current.video.data?.user_state?.last_position_seconds).toBe(
				42,
			),
		);
		expect(bodies).toEqual([
			{ video_id: 7, position_seconds: 42, completed: false },
		]);
		expect(result.current.video.data?.user_state).toMatchObject({
			watch_later: false,
			watched_at: "2026-01-01T00:00:00Z",
		});
		expect(contexts).toContainEqual({
			path: "video.updateWatchProgress",
			keepalive: true,
		});
		// Confirmed: the mirror is gone.
		expect(readLocalWatchProgress("u1", 7)).toBeNull();
		expect(queryClient.getQueryState(listPageKey)?.isInvalidated).toBe(true);
		expect(queryClient.getQueryState(statsKey)?.isInvalidated).toBe(false);
	});

	it("keeps the mirror when the server never confirms the write", async () => {
		let failures = 0;
		const { wrapper } = harness(async (_url, options) => {
			if (options?.method === "POST") {
				failures += 1;
				throw new Error("server restarting");
			}
			return json(recording);
		}, []);
		const { result } = renderHook(() => useWatchProgressWriter(7), { wrapper });

		act(() => {
			result.current(61.5, false);
		});
		await waitFor(() => expect(failures).toBe(1));
		expect(readLocalWatchProgress("u1", 7)).toMatchObject({
			positionSeconds: 61.5,
			completed: false,
		});
	});

	it("clamps and ignores what cannot be saved", async () => {
		let posts = 0;
		const { wrapper } = harness(async (_url, options) => {
			if (options?.method === "POST") posts += 1;
			return json(recording);
		}, []);
		const { result } = renderHook(() => useWatchProgressWriter(7), { wrapper });

		act(() => {
			result.current(Number.NaN, false);
		});
		expect(readLocalWatchProgress("u1", 7)).toBeNull();
		act(() => {
			result.current(-3, true);
		});
		expect(readLocalWatchProgress("u1", 7)).toMatchObject({
			positionSeconds: 0,
			completed: true,
		});
		await waitFor(() => expect(posts).toBe(1));
	});
});
