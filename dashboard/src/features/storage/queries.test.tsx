// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";

// Subscriptions cannot ride the fake http transport; capture their handlers
// so a pushed transition can be replayed by hand.
const subscriptions = vi.hoisted(() => ({
	onData: [] as (() => Promise<void>)[],
	onStarted: [] as (() => Promise<void>)[],
}));
vi.mock("@trpc/tanstack-react-query", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("@trpc/tanstack-react-query")>();
	return {
		...actual,
		useSubscription: (opts: {
			onData: () => Promise<void>;
			onStarted: () => Promise<void>;
		}) => {
			subscriptions.onData.push(opts.onData);
			subscriptions.onStarted.push(opts.onStarted);
		},
	};
});

import {
	storageUnreadable,
	storageUnwritable,
	useAdoptStorage,
	useLiveStorageStatus,
	useStorageDetails,
	useStorageStatus,
} from "./queries";

afterEach(() => {
	cleanup();
	subscriptions.onData = [];
	subscriptions.onStarted = [];
});

function harness(
	fetch: (url: string, options?: RequestInit) => Promise<Response>,
) {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: { retry: false, staleTime: Infinity, gcTime: 0 },
			mutations: { retry: false },
		},
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
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

function json(data: unknown) {
	return new Response(JSON.stringify({ result: { data } }), {
		headers: { "Content-Type": "application/json" },
	});
}

const unattached = {
	state: "unattached",
	reason: "marker missing",
	backend: "local",
	location: "/mnt/data",
	storage_id: "abc",
	checked_at: "2026-09-08T12:00:00Z",
};

describe("storage readiness hooks", () => {
	it("classifies which states stop playback", () => {
		expect(storageUnreadable("unattached")).toBe(true);
		expect(storageUnreadable("unreachable")).toBe(true);
		expect(storageUnreadable("read_only")).toBe(false);
		expect(storageUnreadable("full")).toBe(false);
		expect(storageUnreadable("attached")).toBe(false);
		expect(storageUnreadable(undefined)).toBe(false);
	});

	it("pauses recording on full storage while playback remains available", () => {
		expect(storageUnwritable("full")).toBe(true);
		expect(storageUnreadable("full")).toBe(false);
	});

	it("does not ask for owner details until enabled", async () => {
		const calls: string[] = [];
		const { wrapper } = harness(async (url) => {
			calls.push(new URL(url).pathname);
			return json(unattached);
		});
		const { result, rerender } = renderHook(
			({ enabled }: { enabled: boolean }) => useStorageDetails(enabled),
			{ wrapper, initialProps: { enabled: false } },
		);
		expect(result.current.fetchStatus).toBe("idle");
		expect(calls).toEqual([]);
		rerender({ enabled: true });
		await waitFor(() =>
			expect(result.current.data?.reason).toBe("marker missing"),
		);
		expect(calls).toEqual(["/trpc/storage.details"]);
	});

	it("adopting refetches the status and the details", async () => {
		let state = "unattached";
		const procedures: string[] = [];
		const { wrapper } = harness(async (url, options) => {
			const procedure = new URL(url).pathname;
			procedures.push(procedure);
			if (options?.method === "POST") {
				state = "attached";
				return json({
					...unattached,
					state,
					reason: "",
					scan_status: "scheduled",
				});
			}
			return json({ ...unattached, state });
		});
		const { result } = renderHook(
			() => ({
				status: useStorageStatus(),
				details: useStorageDetails(),
				adopt: useAdoptStorage(),
			}),
			{ wrapper },
		);
		await waitFor(() =>
			expect(result.current.status.data?.state).toBe("unattached"),
		);
		await waitFor(() =>
			expect(result.current.details.data?.state).toBe("unattached"),
		);
		await act(async () => {
			await result.current.adopt.mutateAsync();
		});
		await waitFor(() =>
			expect(result.current.status.data?.state).toBe("attached"),
		);
		await waitFor(() =>
			expect(result.current.details.data?.state).toBe("attached"),
		);
		expect(procedures.filter((p) => p === "/trpc/storage.adopt")).toHaveLength(
			1,
		);
	});

	it("a pushed transition refetches the status", async () => {
		let state = "attached";
		const { wrapper } = harness(async () =>
			json({ state, checked_at: "2026-09-08T12:00:00Z" }),
		);
		const { result } = renderHook(
			() => {
				useLiveStorageStatus();
				return useStorageStatus();
			},
			{ wrapper },
		);
		await waitFor(() => expect(result.current.data?.state).toBe("attached"));
		expect(subscriptions.onData.length).toBeGreaterThan(0);
		state = "unattached";
		await act(async () => {
			await subscriptions.onData.at(-1)?.();
		});
		await waitFor(() => expect(result.current.data?.state).toBe("unattached"));
	});
});

it.each([
	true,
	false,
])("resynchronizes missed transitions on connection (owner details enabled: %s)", async (owner) => {
	let state = "unattached";
	const calls: string[] = [];
	const { wrapper } = harness(async (url) => {
		calls.push(new URL(url).pathname);
		return json({ ...unattached, state });
	});
	const { result } = renderHook(
		() => {
			useLiveStorageStatus();
			return { status: useStorageStatus(), details: useStorageDetails(owner) };
		},
		{ wrapper },
	);
	await waitFor(() =>
		expect(result.current.status.data?.state).toBe("unattached"),
	);
	if (owner)
		await waitFor(() =>
			expect(result.current.details.data?.state).toBe("unattached"),
		);
	// The server changes while SSE is disconnected; there is no onData event.
	state = "attached";
	await act(async () => {
		await subscriptions.onStarted.at(-1)?.();
	});
	await waitFor(() =>
		expect(result.current.status.data?.state).toBe("attached"),
	);
	if (owner) expect(result.current.details.data?.state).toBe("attached");
	else expect(calls).not.toContain("/trpc/storage.details");
	// A later reconnect must resync again, even while the query is fresh.
	state = "read_only";
	await act(async () => {
		await subscriptions.onStarted.at(-1)?.();
	});
	await waitFor(() =>
		expect(result.current.status.data?.state).toBe("read_only"),
	);
});

it.each([
	"onStarted",
	"onData",
] as const)("replaces a stale initial request on %s", async (event) => {
	let resolveFirst!: (response: Response) => void;
	const first = new Promise<Response>((resolve) => {
		resolveFirst = resolve;
	});
	let calls = 0;
	const { wrapper } = harness(async () => {
		calls++;
		return calls === 1 ? first : json({ ...unattached, state: "attached" });
	});
	const { result } = renderHook(
		() => {
			useLiveStorageStatus();
			return useStorageStatus();
		},
		{ wrapper },
	);
	await waitFor(() => expect(calls).toBe(1));
	await act(async () => {
		await subscriptions[event].at(-1)?.();
	});
	await waitFor(() => expect(result.current.data?.state).toBe("attached"));
	await act(async () => {
		resolveFirst(json(unattached));
		await first;
	});
	expect(result.current.data?.state).toBe("attached");
	expect(calls).toBe(2);
});
