// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { useTestRecordingWebhookDelivery } from "./queries";

vi.mock("@/api/trpc", () => ({
	useTRPC: () => ({
		recordingWebhook: {
			testDelivery: {
				mutationOptions: (options: { onSuccess?: () => void }) => ({
					mutationFn: async () => ({ ok: true, status: 200 }),
					...options,
				}),
			},
			deliveries: {
				pathKey: () => [["recordingWebhook", "deliveries"]],
			},
			config: {
				pathKey: () => [["recordingWebhook", "config"]],
			},
		},
	}),
}));

describe("recording webhook queries", () => {
	it("refreshes deliveries and config after a test delivery", async () => {
		const queryClient = new QueryClient({
			defaultOptions: { mutations: { retry: false } },
		});
		const invalidate = vi.spyOn(queryClient, "invalidateQueries");
		const wrapper = ({ children }: { children: ReactNode }) =>
			createElement(QueryClientProvider, { client: queryClient }, children);

		const { result } = renderHook(() => useTestRecordingWebhookDelivery(), {
			wrapper,
		});

		act(() => result.current.mutate());

		await waitFor(() => {
			expect(invalidate).toHaveBeenCalledWith({
				queryKey: [["recordingWebhook", "deliveries"]],
			});
		});
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: [["recordingWebhook", "config"]],
		});
	});
});
