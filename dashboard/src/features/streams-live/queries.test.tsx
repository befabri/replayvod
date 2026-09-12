// @vitest-environment jsdom
import {
	QueryClient,
	QueryClientProvider,
	useQueries,
} from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createTRPCOptionsProxy } from "@trpc/tanstack-react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { liveRenditionsOptions } from "@/features/videos/queries";

type Handlers = {
	onStarted: () => Promise<void>;
	onData: (event: {
		broadcaster_id: string;
		kind: "online" | "offline";
	}) => void;
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
	broadcasterLiveOptions,
	useLiveSet,
	useLiveStreamStatus,
} from "./queries";

afterEach(cleanup);

function harness() {
	const pending: ((ids: string[]) => void)[] = [];
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false, gcTime: 0 } },
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: () =>
					new Promise<Response>((resolve) => {
						pending.push((ids) =>
							resolve(
								new Response(JSON.stringify({ result: { data: ids } }), {
									headers: { "Content-Type": "application/json" },
								}),
							),
						);
					}),
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
			useLiveStreamStatus();
			return [useLiveSet(), useLiveSet()];
		},
		{ wrapper },
	);
	return { ...view, pending };
}

it("preserves events arriving during reconnect and shares the result with every consumer", async () => {
	const { result, pending } = harness();
	await waitFor(() => expect(pending).toHaveLength(1));
	act(() => pending[0](["old"]));
	await waitFor(() => expect(result.current[0].has("old")).toBe(true));
	let reconnect: Promise<void> | undefined;
	act(() => {
		reconnect = subscription.current?.onStarted();
	});
	await waitFor(() => expect(pending).toHaveLength(2));
	act(() => {
		subscription.current?.onData({ broadcaster_id: "new", kind: "online" });
		subscription.current?.onData({ broadcaster_id: "old", kind: "offline" });
	});
	await act(async () => {
		pending[1](["old", "snapshot-only"]);
		await reconnect;
	});
	await waitFor(() =>
		expect([...result.current[0]]).toEqual(["snapshot-only", "new"]),
	);
	expect([...result.current[1]]).toEqual(["snapshot-only", "new"]);
	expect(pending).toHaveLength(2);
});

it("drops pre-reconnect deltas that the new snapshot supersedes", async () => {
	const { result, pending } = harness();
	await waitFor(() => expect(pending).toHaveLength(1));
	act(() => pending[0]([]));
	act(() =>
		subscription.current?.onData({ broadcaster_id: "stale", kind: "online" }),
	);
	act(() => {
		void subscription.current?.onStarted();
	});
	await waitFor(() => expect(pending).toHaveLength(2));
	act(() => pending[1](["fresh"]));
	await waitFor(() => expect([...result.current[0]]).toEqual(["fresh"]));
});

it("ignores a cancelled snapshot that finishes after a newer reconnect", async () => {
	const { result, pending } = harness();
	await waitFor(() => expect(pending).toHaveLength(1));
	act(() => {
		void subscription.current?.onStarted();
	});
	await waitFor(() => expect(pending).toHaveLength(2));
	act(() => {
		void subscription.current?.onStarted();
	});
	await waitFor(() => expect(pending).toHaveLength(3));
	act(() =>
		subscription.current?.onData({ broadcaster_id: "event", kind: "online" }),
	);
	act(() => pending[2](["current"]));
	await waitFor(() =>
		expect([...result.current[0]]).toEqual(["current", "event"]),
	);
	await act(async () => {
		pending[0](["stale-initial"]);
		pending[1](["stale-reconnect"]);
		await new Promise((resolve) => setTimeout(resolve, 10));
	});
	expect([...result.current[0]]).toEqual(["current", "event"]);
});

it("refreshes the broadcaster and both codec lists on a live event, and recovers missed changes on reconnect", async () => {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false, gcTime: Infinity } },
	});
	let live = true;
	let height = 1440;
	const calls: string[] = [];
	const client = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: async (input) => {
					const url = new URL(String(input));
					const proc = url.pathname.split("/").at(-1);
					const args = JSON.parse(url.searchParams.get("input") ?? "{}");
					calls.push(
						`${proc}:${args.broadcaster_id ?? ""}:${args.force_h264 ?? ""}`,
					);
					const data =
						proc === "stream.isLive"
							? live
							: proc === "video.liveRenditions"
								? {
										anonymous: false,
										renditions: [
											{ height, codec: args.force_h264 ? "h264" : "h265" },
										],
									}
								: [];
					return new Response(JSON.stringify({ result: { data } }), {
						headers: { "Content-Type": "application/json" },
					});
				},
			}),
		],
	});
	const trpc = createTRPCOptionsProxy<AppRouter>({ client, queryClient });
	const b1 = broadcasterLiveOptions(trpc, "b1");
	const b2 = broadcasterLiveOptions(trpc, "b2");
	const h264 = liveRenditionsOptions(trpc, "b1", true);
	const hevc = liveRenditionsOptions(trpc, "b1", false);
	const other = liveRenditionsOptions(trpc, "b2", false);
	const { unmount } = renderHook(
		() => {
			useLiveStreamStatus();
			return useQueries({ queries: [b1, b2, h264, hevc, other] });
		},
		{
			wrapper: ({ children }) => (
				<QueryClientProvider client={queryClient}>
					<TRPCProvider trpcClient={client} queryClient={queryClient}>
						{children}
					</TRPCProvider>
				</QueryClientProvider>
			),
		},
	);
	await waitFor(() =>
		expect(
			queryClient.getQueryData(other.queryKey)?.renditions?.[0].height,
		).toBe(1440),
	);
	live = false;
	height = 720;
	calls.length = 0;
	act(() =>
		subscription.current?.onData({ broadcaster_id: "b1", kind: "offline" }),
	);
	await waitFor(() => {
		expect(queryClient.getQueryData(b1.queryKey)).toBe(false);
		for (const options of [h264, hevc])
			expect(
				queryClient.getQueryData(options.queryKey)?.renditions?.[0].height,
			).toBe(720);
	});
	expect(queryClient.getQueryData(b2.queryKey)).toBe(true);
	expect(queryClient.getQueryData(other.queryKey)?.renditions?.[0].height).toBe(
		1440,
	);
	expect(calls.sort()).toEqual([
		"stream.isLive:b1:",
		"video.liveRenditions:b1:false",
		"video.liveRenditions:b1:true",
	]);
	await act(async () => {
		await subscription.current?.onStarted();
	});
	expect(queryClient.getQueryData(b2.queryKey)).toBe(false);
	expect(queryClient.getQueryData(other.queryKey)?.renditions?.[0].height).toBe(
		720,
	);
	unmount();
	queryClient.clear();
});
