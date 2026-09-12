// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";

type Handlers = {
	onStarted: () => Promise<unknown>;
	onData: () => Promise<unknown>;
};
const subscription = vi.hoisted(() => ({
	current: undefined as Handlers | undefined,
}));
vi.mock("@trpc/tanstack-react-query", async (original) => ({
	...(await original<typeof import("@trpc/tanstack-react-query")>()),
	useSubscription: (options: Handlers) => {
		subscription.current = options;
	},
}));

import {
	useHistoryCounts,
	useInfiniteVideoPages,
	useLiveVideoRemovals,
	useVideo,
} from "./queries";

afterEach(() => {
	cleanup();
	vi.useRealTimers();
});

function harness() {
	vi.useFakeTimers();
	let removed = false;
	const calls: Record<string, number> = {};
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false, gcTime: 0 } },
	});
	const video = (id: number) => ({
		id,
		status: "DONE",
		deleted_at: "2026-09-01T00:00:00Z",
		deletion_kind: removed ? "manual" : "missing",
		delete_requested_at:
			id === 95 && !removed ? "2026-09-12T00:00:00Z" : undefined,
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: async (input) => {
					const url = new URL(String(input));
					const path = url.pathname.split("/").at(-1) ?? "";
					calls[path] = (calls[path] ?? 0) + 1;
					const cursor = Number(
						JSON.parse(url.searchParams.get("input") ?? "{}").cursor?.id ?? 0,
					);
					const data =
						path === "video.listPage"
							? {
									items: [video(95 + cursor)],
									next_cursor:
										cursor < 9
											? {
													id: cursor + 1,
													start_download_at: "2026-09-01T00:00:00Z",
												}
											: null,
								}
							: path === "video.getById"
								? video(95)
								: {
										all: {
											on_disk: 0,
											removed: 10,
											unavailable: removed ? 9 : 10,
										},
									};
					return new Response(JSON.stringify({ result: { data } }), {
						headers: { "Content-Type": "application/json" },
					});
				},
			}),
		],
	});
	const wrapper = ({ children }: { children: ReactNode }) => (
		<QueryClientProvider client={queryClient}>
			<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
				{children}
			</TRPCProvider>
		</QueryClientProvider>
	);
	const view = renderHook(
		() => {
			useLiveVideoRemovals();
			return {
				list: useInfiniteVideoPages(),
				video: useVideo(95),
				counts: useHistoryCounts(),
			};
		},
		{ wrapper },
	);
	return {
		...view,
		calls,
		remove: () => {
			removed = true;
		},
	};
}

it("does not poll loaded pages while deletion waits, and refreshes lists, detail and counts on commit", async () => {
	const { result, calls, remove } = harness();
	await act(async () => {
		await vi.advanceTimersByTimeAsync(10);
	});
	expect(result.current.list.data?.pages).toHaveLength(1);
	for (let page = 1; page < 10; page++)
		await act(async () => {
			await result.current.list.fetchNextPage();
			await vi.advanceTimersByTimeAsync(1);
		});
	expect(result.current.list.error).toBeNull();
	expect(result.current.list.data?.pages).toHaveLength(10);
	expect(result.current.video.data?.delete_requested_at).toBeTruthy();
	const before = { ...calls };
	await act(async () => {
		await vi.advanceTimersByTimeAsync(60_000);
	});
	expect(calls["video.listPage"]).toBe(before["video.listPage"]);
	expect(calls["video.getById"]).toBe(before["video.getById"]);
	remove();
	await act(async () => {
		await subscription.current?.onData();
		await vi.advanceTimersByTimeAsync(1);
	});
	expect(
		result.current.list.data?.pages[0].items[0].delete_requested_at,
	).toBeUndefined();
	expect(result.current.video.data?.deletion_kind).toBe("manual");
	expect(result.current.counts.data?.all.unavailable).toBe(9);
	expect(calls["video.listPage"] - before["video.listPage"]).toBe(10);
	const after = { ...calls };
	await act(async () => {
		await vi.advanceTimersByTimeAsync(60_000);
	});
	expect(calls["video.listPage"]).toBe(after["video.listPage"]);
	expect(calls["video.getById"]).toBe(after["video.getById"]);
});

it("recovers a deletion completed while disconnected without a pushed event", async () => {
	const { result, remove } = harness();
	await act(async () => {
		await vi.advanceTimersByTimeAsync(10);
	});
	expect(result.current.video.data?.delete_requested_at).toBeTruthy();
	remove();
	await act(async () => {
		await subscription.current?.onStarted();
		await vi.advanceTimersByTimeAsync(1);
	});
	expect(result.current.video.data?.delete_requested_at).toBeUndefined();
	expect(result.current.list.data?.pages[0].items[0].deletion_kind).toBe(
		"manual",
	);
	expect(result.current.counts.data?.all.unavailable).toBe(9);
});
