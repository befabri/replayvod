// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { FetchLogsResponse } from "@/api/generated/trpc";
import { TRPCProvider } from "@/api/trpc";
import { createMockTrpcClient, neverResolves } from "@/test/trpc-mock";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/integrations/tanstack-query/root-provider", () => ({
	trpcClient: {},
}));

import { Route } from "./logs";

afterEach(cleanup);

const firstPage: FetchLogsResponse = {
	total: 51,
	data: [
		{
			id: 1,
			fetch_type: "get_streams",
			status: 200,
			duration_ms: 25,
			fetched_at: "2026-10-01T12:00:00Z",
		},
	],
};

function renderLogsPage(fetchLogs: () => Promise<FetchLogsResponse>) {
	const LogsPage = Route.options.component;
	if (!LogsPage) throw new Error("the logs route has no component");
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	const trpcClient = createMockTrpcClient({
		system: {
			eventLogs: () => ({ total: 0, data: [] }),
			events: neverResolves,
			fetchLogs,
		},
	});
	render(
		<QueryClientProvider client={queryClient}>
			<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
				<LogsPage />
			</TRPCProvider>
		</QueryClientProvider>,
	);
	return queryClient;
}

function showApiLogs() {
	fireEvent.click(screen.getByText("logs.source_events"));
	const option = screen.getByRole("option", { name: "logs.source_api" });
	fireEvent.pointerDown(option, { pointerType: "mouse", button: 0 });
	fireEvent.click(option);
}

describe("API logs", () => {
	it("keeps the loaded table and pager available after a refetch fails", async () => {
		const fetchLogs = vi
			.fn<() => Promise<FetchLogsResponse>>()
			.mockResolvedValueOnce(firstPage)
			.mockRejectedValueOnce(new Error("refetch failed"))
			.mockResolvedValue({ total: 51, data: [] });
		const queryClient = renderLogsPage(fetchLogs);
		showApiLogs();
		await screen.findByText(/common\.page/);

		await act(async () => {
			await queryClient.invalidateQueries();
		});
		expect(await screen.findByText(/refetch failed/)).toBeTruthy();
		expect(screen.getByRole("table")).toBeTruthy();
		expect(screen.getByText("get_streams")).toBeTruthy();
		fireEvent.click(screen.getByRole("button", { name: "common.next" }));
		await waitFor(() => expect(fetchLogs).toHaveBeenCalledTimes(3));
		await waitFor(() =>
			expect(screen.queryByText(/refetch failed/)).toBeNull(),
		);
	});

	it("can go back to loaded data when the next page fails", async () => {
		const fetchLogs = vi
			.fn<() => Promise<FetchLogsResponse>>()
			.mockResolvedValueOnce(firstPage)
			.mockRejectedValueOnce(new Error("next page failed"))
			.mockResolvedValue(firstPage);
		renderLogsPage(fetchLogs);
		showApiLogs();
		await screen.findByText("get_streams");
		fireEvent.click(screen.getByRole("button", { name: "common.next" }));
		await screen.findByText(/next page failed/);

		// Placeholder rows belong to the previous page and must not be passed
		// off as results from the failed page. Keep a way back to that page.
		expect(screen.queryByRole("table")).toBeNull();
		expect(
			screen
				.getByRole("button", { name: "common.next" })
				.hasAttribute("disabled"),
		).toBe(true);
		fireEvent.click(screen.getByRole("button", { name: "common.previous" }));
		expect(await screen.findByText("get_streams")).toBeTruthy();
		expect(screen.queryByText(/next page failed/)).toBeNull();
	});

	it("shows the initial error without an empty table or pager", async () => {
		renderLogsPage(() => Promise.reject(new Error("first page failed")));
		showApiLogs();
		expect(await screen.findByText(/first page failed/)).toBeTruthy();
		expect(screen.queryByRole("table")).toBeNull();
		expect(screen.queryByRole("button", { name: "common.next" })).toBeNull();
	});

	it("holds the pager back until the first page arrives", async () => {
		let deliver: (page: FetchLogsResponse) => void = () => {};
		const fetchLogs = vi.fn(
			() =>
				new Promise<FetchLogsResponse>((resolve) => {
					deliver = resolve;
				}),
		);
		renderLogsPage(fetchLogs);
		showApiLogs();
		await waitFor(() => expect(fetchLogs).toHaveBeenCalled());

		// A pager now would read as an empty log before anything has loaded.
		expect(screen.queryByText(/common\.page/)).toBeNull();
		expect(screen.queryByRole("button", { name: "common.next" })).toBeNull();

		deliver({ total: 0, data: [] });
		expect(await screen.findByText(/common\.page/)).toBeTruthy();
		expect(screen.getByRole("button", { name: "common.next" })).toBeTruthy();
	});
});
