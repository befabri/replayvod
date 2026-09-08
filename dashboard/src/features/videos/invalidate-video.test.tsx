// @vitest-environment jsdom

import {
	QueryClient,
	QueryClientProvider,
	type QueryKey,
} from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { type ReactNode, StrictMode } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { type AppRouter, TRPCProvider, useTRPC } from "@/api/trpc";
import { VIDEO_LIST_CACHES, videoCaches } from "./cache";
import { useInvalidateVideo } from "./queries";

afterEach(cleanup);

function harness() {
	const clients = {
		queryClient: new QueryClient(),
		trpcClient: createTRPCClient<AppRouter>({
			links: [httpLink({ url: "http://example.test/trpc" })],
		}),
	};
	const wrapper = ({ children }: { children: ReactNode }) => (
		<StrictMode>
			<QueryClientProvider client={clients.queryClient}>
				<TRPCProvider {...clients}>{children}</TRPCProvider>
			</QueryClientProvider>
		</StrictMode>
	);
	return { clients, wrapper };
}

describe("useInvalidateVideo", () => {
	it("keeps its identity across renders and invalidates the current recording and every list", async () => {
		const { clients, wrapper } = harness();
		const { queryClient } = clients;
		const { result, rerender } = renderHook(
			({ id }) => ({ invalidate: useInvalidateVideo(id), trpc: useTRPC() }),
			{ wrapper, initialProps: { id: 7 } },
		);
		const initial = result.current.invalidate;
		rerender({ id: 7 });
		expect(result.current.invalidate).toBe(initial);
		rerender({ id: 7 });
		expect(result.current.invalidate).toBe(initial);

		rerender({ id: 8 });
		expect(result.current.invalidate).not.toBe(initial);
		const { trpc } = result.current;
		const oldKey = trpc.video.getById.queryKey({ id: 7 });
		const currentKey = trpc.video.getById.queryKey({ id: 8 });
		const unrelatedKey = [["channel", "list"]];
		const caches = videoCaches(trpc);
		const listKeys = VIDEO_LIST_CACHES.map((name) => caches[name].pathKey);
		for (const key of [oldKey, currentKey, unrelatedKey, ...listKeys]) {
			queryClient.setQueryData(key, {});
		}

		await act(() => result.current.invalidate());
		for (const key of [currentKey, ...listKeys]) {
			expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
		}
		expect(queryClient.getQueryState(oldKey)?.isInvalidated).toBe(false);
		expect(queryClient.getQueryState(unrelatedKey)?.isInvalidated).toBe(false);
	});

	it("uses the replacement query client when the provider changes", async () => {
		const { clients, wrapper } = harness();
		const { result, rerender } = renderHook(
			() => ({ invalidate: useInvalidateVideo(7), trpc: useTRPC() }),
			{ wrapper },
		);
		const initial = result.current.invalidate;
		const oldClient = clients.queryClient;
		// Only cache invalidation matters here, so the sentinel data can be empty.
		const key: QueryKey = result.current.trpc.video.getById.queryKey({ id: 7 });
		oldClient.setQueryData(key, {});
		clients.queryClient = new QueryClient();
		clients.queryClient.setQueryData(key, {});
		rerender();
		expect(result.current.invalidate).not.toBe(initial);

		await act(() => result.current.invalidate());
		expect(clients.queryClient.getQueryState(key)?.isInvalidated).toBe(true);
		expect(oldClient.getQueryState(key)?.isInvalidated).toBe(false);
	});
});
