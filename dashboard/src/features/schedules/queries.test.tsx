// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { useSetSchedulesPaused } from "./queries";

// Shared, hoisted toggle so the mocked mutationFn can be made to fail per test
// (vi.mock is hoisted above module scope, so it can't close over a plain let).
const mockState = vi.hoisted(() => ({ fail: false }));

vi.mock("@/api/trpc", () => ({
	useTRPC: () => ({
		schedule: {
			setPaused: {
				// Mirror tRPC's mutationOptions: provide the mutationFn and spread
				// the caller's onMutate/onError/onSettled so the optimistic logic runs.
				mutationOptions: (options: Record<string, unknown>) => ({
					mutationFn: async (vars: { paused: boolean }) => {
						if (mockState.fail) throw new Error("boom");
						return { paused: vars.paused };
					},
					...options,
				}),
			},
			pauseState: {
				queryKey: () => ["schedule", "pauseState"],
			},
		},
	}),
}));

const KEY = ["schedule", "pauseState"];

function setup() {
	const queryClient = new QueryClient({
		defaultOptions: { mutations: { retry: false } },
	});
	queryClient.setQueryData(KEY, { paused: false });
	const wrapper = ({ children }: { children: ReactNode }) =>
		createElement(QueryClientProvider, { client: queryClient }, children);
	const { result } = renderHook(() => useSetSchedulesPaused(), { wrapper });
	return { queryClient, result };
}

describe("useSetSchedulesPaused", () => {
	it("optimistically flips the cached pause flag and invalidates on settle", async () => {
		mockState.fail = false;
		const { queryClient, result } = setup();
		const invalidate = vi.spyOn(queryClient, "invalidateQueries");

		act(() => {
			result.current.mutate({ paused: true });
		});

		// Optimistic: the cache flips before the mutation resolves.
		await waitFor(() => {
			expect(queryClient.getQueryData(KEY)).toEqual({ paused: true });
		});
		await waitFor(() => {
			expect(result.current.isSuccess).toBe(true);
		});
		expect(invalidate).toHaveBeenCalledWith({ queryKey: KEY });
	});

	it("rolls back the cached pause flag when the mutation fails", async () => {
		mockState.fail = true;
		const { queryClient, result } = setup();

		act(() => {
			result.current.mutate({ paused: true });
		});

		await waitFor(() => {
			expect(result.current.isError).toBe(true);
		});
		// The optimistic flip is reverted to the pre-mutation value.
		expect(queryClient.getQueryData(KEY)).toEqual({ paused: false });
	});
});
