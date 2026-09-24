// @vitest-environment jsdom

import {
	QueryClient,
	QueryClientProvider,
	useSuspenseQuery,
} from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { TRPCClientError } from "@trpc/client";
import { Component, type ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryBoundary } from "./query-boundary";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));

beforeEach(() => {
	vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
	vi.useRealTimers();
});

function Greeting({
	id = 1,
	load,
	label = "greeting.failed_to_load",
}: {
	id?: number;
	load: () => Promise<string>;
	label?: string;
}) {
	const { data } = useSuspenseQuery({
		queryKey: ["greeting", id],
		queryFn: load,
		meta: label ? { errorLabel: label } : undefined,
	});
	return <p>{data}</p>;
}

class Outer extends Component<{ children: ReactNode }, { error?: Error }> {
	state: { error?: Error } = {};
	static getDerivedStateFromError(error: Error) {
		return { error };
	}
	render() {
		return this.state.error ? (
			<p>outer caught {this.state.error.message}</p>
		) : (
			this.props.children
		);
	}
}

function newQueryClient() {
	return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderBoundary(
	load: () => Promise<string>,
	queryClient = newQueryClient(),
) {
	return render(
		<QueryClientProvider client={queryClient}>
			<Outer>
				<QueryBoundary fallback={<p>skeleton</p>}>
					<Greeting load={load} />
				</QueryBoundary>
			</Outer>
		</QueryClientProvider>,
	);
}

function requestError(message: string) {
	return TRPCClientError.from({
		error: { message, code: -32000, data: { code: "INTERNAL_SERVER_ERROR" } },
	});
}

it("shows the fallback until the query resolves", async () => {
	renderBoundary(async () => "hello");

	expect(screen.getByText("skeleton")).toBeTruthy();
	expect(await screen.findByText("hello")).toBeTruthy();
});

it("shows the error in place and refetches on retry", async () => {
	const load = vi
		.fn<() => Promise<string>>()
		.mockRejectedValueOnce(requestError("boom"))
		.mockResolvedValueOnce("recovered");
	renderBoundary(load);

	expect((await screen.findByRole("alert")).textContent).toContain(
		"greeting.failed_to_load: boom",
	);
	fireEvent.click(screen.getByRole("button", { name: "common.retry" }));

	expect(await screen.findByText("recovered")).toBeTruthy();
	expect(load).toHaveBeenCalledTimes(2);
});

it("labels the error after the query that failed, not the boundary", async () => {
	render(
		<QueryClientProvider client={newQueryClient()}>
			<QueryBoundary fallback={<p>skeleton</p>}>
				<Greeting load={() => Promise.reject(requestError("boom"))} label="" />
			</QueryBoundary>
		</QueryClientProvider>,
	);

	const alert = await screen.findByRole("alert");
	expect(alert.textContent).toContain("common.failed_to_load: boom");
	expect(alert.textContent).not.toContain("greeting");
});

it("leaves errors that are not failed requests to the boundary above", async () => {
	renderBoundary(async () => {
		throw new TypeError("render bug");
	});

	expect(await screen.findByText("outer caught render bug")).toBeTruthy();
	expect(screen.queryByRole("alert")).toBeNull();
});

// Several sections read the same query, each behind its own boundary. A retry
// in one of them must clear the others once the query is back, or they stay on
// an error the page no longer has.
it("recovers every boundary showing a query once any of them retries", async () => {
	const load = vi
		.fn<() => Promise<string>>()
		.mockRejectedValueOnce(requestError("boom"))
		.mockResolvedValueOnce("recovered");
	render(
		<QueryClientProvider client={newQueryClient()}>
			<section aria-label="first">
				<QueryBoundary fallback={<p>skeleton</p>}>
					<Greeting load={load} />
				</QueryBoundary>
			</section>
			<section aria-label="second">
				<QueryBoundary fallback={<p>skeleton</p>}>
					<Greeting load={load} />
				</QueryBoundary>
			</section>
		</QueryClientProvider>,
	);

	expect(await screen.findAllByRole("alert")).toHaveLength(2);
	fireEvent.click(
		screen.getAllByRole("button", { name: "common.retry" })[0] as HTMLElement,
	);

	await waitFor(() => expect(screen.getAllByText("recovered")).toHaveLength(2));
	expect(screen.queryByRole("alert")).toBeNull();
	expect(load).toHaveBeenCalledTimes(2);
});

// Suspense queries never retry a cached error on mount, so a section opened
// after the failure would show it without asking again.
it("asks once more when it opens on an error from before it mounted", async () => {
	vi.useFakeTimers({ toFake: ["Date"] });
	vi.setSystemTime(1_000);
	const queryClient = newQueryClient();
	await queryClient.prefetchQuery({
		queryKey: ["greeting", 1],
		queryFn: () => Promise.reject(requestError("earlier")),
	});
	expect(queryClient.getQueryState(["greeting", 1])?.status).toBe("error");
	vi.setSystemTime(11_000);

	const load = vi.fn<() => Promise<string>>().mockResolvedValueOnce("fresh");
	renderBoundary(load, queryClient);

	expect(await screen.findByText("fresh")).toBeTruthy();
	expect(load).toHaveBeenCalledTimes(1);
});

it("does not keep refetching when the error it opened on happens again", async () => {
	vi.useFakeTimers({ toFake: ["Date"] });
	vi.setSystemTime(1_000);
	const queryClient = newQueryClient();
	await queryClient.prefetchQuery({
		queryKey: ["greeting", 1],
		queryFn: () => Promise.reject(requestError("earlier")),
	});
	vi.setSystemTime(11_000);

	const load = vi
		.fn<() => Promise<string>>()
		.mockRejectedValue(requestError("still down"));
	renderBoundary(load, queryClient);

	await vi.waitFor(() => expect(load).toHaveBeenCalledTimes(1));
	expect(
		await screen.findByText("greeting.failed_to_load: earlier", {
			exact: false,
		}),
	).toBeTruthy();
	await new Promise((resolve) => setTimeout(resolve, 50));
	expect(load).toHaveBeenCalledTimes(1);
});
