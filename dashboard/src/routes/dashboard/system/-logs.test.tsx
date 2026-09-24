// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
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
}

function showApiLogs() {
	fireEvent.click(screen.getByText("logs.source_events"));
	const option = screen.getByRole("option", { name: "logs.source_api" });
	fireEvent.pointerDown(option, { pointerType: "mouse", button: 0 });
	fireEvent.click(option);
}

describe("API logs", () => {
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
