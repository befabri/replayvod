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
import { createTRPCClient, httpLink } from "@trpc/client";
import { createTRPCOptionsProxy } from "@trpc/tanstack-react-query";
import { type ReactNode, StrictMode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/integrations/tanstack-query/root-provider", () => ({
	trpcClient: {},
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

import { DirectDownloadForm } from "./DirectDownloadForm";

afterEach(cleanup);

function harness(lookup: (id: string) => Promise<boolean> = async () => true) {
	const onClose = vi.fn();
	const requests: string[] = [];
	let finish!: (response: Response) => void;
	let liveLookup = lookup;
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false, gcTime: 0 } },
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: async (input) => {
					const path = new URL(String(input)).pathname;
					requests.push(path);
					if (path.endsWith("stream.isLive")) {
						const args = JSON.parse(
							new URL(String(input)).searchParams.get("input") ?? "{}",
						);
						return new Response(
							JSON.stringify({
								result: { data: await liveLookup(args.broadcaster_id) },
							}),
							{
								headers: { "Content-Type": "application/json" },
							},
						);
					}
					if (path.endsWith("triggerDownload"))
						return new Promise<Response>((resolve) => {
							finish = resolve;
						});
					return new Response(
						JSON.stringify({
							result: {
								data: {
									anonymous: false,
									renditions: [{ height: 1080, codec: "h264" }],
								},
							},
						}),
						{ headers: { "Content-Type": "application/json" } },
					);
				},
			}),
		],
	});
	const trpc = createTRPCOptionsProxy<AppRouter>({
		client: trpcClient,
		queryClient,
	});
	const wrapper = ({ children }: { children: ReactNode }) => (
		<StrictMode>
			<QueryClientProvider client={queryClient}>
				<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
					{children}
				</TRPCProvider>
			</QueryClientProvider>
		</StrictMode>
	);
	const view = render(
		<DirectDownloadForm broadcasterId="b1" onClose={onClose} />,
		{ wrapper },
	);
	return {
		...view,
		requests,
		onClose,
		queryClient,
		trpc,
		setLookup: (next: typeof lookup) => {
			liveLookup = next;
		},
		setBroadcaster: (broadcasterId: string) =>
			view.rerender(
				<DirectDownloadForm broadcasterId={broadcasterId} onClose={onClose} />,
			),
		finish: (ok: boolean) =>
			finish(
				new Response(
					JSON.stringify(
						ok
							? { result: { data: { job_id: "j1" } } }
							: {
									error: {
										message: "Already recording",
										code: -32009,
										data: { code: "CONFLICT", httpStatus: 409 },
									},
								},
					),
					{
						status: ok ? 200 : 409,
						headers: { "Content-Type": "application/json" },
					},
				),
			),
	};
}

it("keeps failed submissions open, unlocks the form, and supports retry", async () => {
	const view = harness();
	const submit = () =>
		screen.getByRole("button", {
			name: "videos.trigger_submit",
		}) as HTMLButtonElement;
	await waitFor(() => expect(submit().disabled).toBe(false));
	fireEvent.click(submit());
	await waitFor(() =>
		expect(
			view.requests.filter((path) => path.endsWith("triggerDownload")),
		).toHaveLength(1),
	);
	expect(
		(screen.getByRole("button", { name: "common.saving" }) as HTMLButtonElement)
			.disabled,
	).toBe(true);
	expect(
		(screen.getByRole("button", { name: "common.cancel" }) as HTMLButtonElement)
			.disabled,
	).toBe(true);
	view.finish(false);
	await waitFor(() =>
		expect(screen.getByRole("alert").textContent).toBe("Already recording"),
	);
	expect(view.onClose).not.toHaveBeenCalled();
	await waitFor(() => expect(submit().disabled).toBe(false));
	fireEvent.click(submit());
	await waitFor(() =>
		expect(
			view.requests.filter((path) => path.endsWith("triggerDownload")),
		).toHaveLength(2),
	);
	view.finish(true);
	await waitFor(() => expect(view.onClose).toHaveBeenCalledTimes(1));
});

it("blocks confirmed offline channels, including programmatic submissions, and recovers on recheck", async () => {
	const view = harness(async () => false);
	await screen.findByText("videos.download.offline");
	expect(
		(
			screen.getByRole("button", {
				name: "videos.trigger_submit",
			}) as HTMLButtonElement
		).disabled,
	).toBe(true);
	fireEvent.submit(
		screen
			.getByRole("button", { name: "videos.trigger_submit" })
			.closest("form") as HTMLFormElement,
	);
	expect(view.requests.some((path) => path.endsWith("liveRenditions"))).toBe(
		false,
	);
	view.setLookup(async () => true);
	fireEvent.click(
		screen.getByRole("button", { name: "videos.download.check_again" }),
	);
	await waitFor(() =>
		expect(
			(
				screen.getByRole("button", {
					name: "videos.trigger_submit",
				}) as HTMLButtonElement
			).disabled,
		).toBe(false),
	);
	// A newer cache verdict wins even if React has not rendered it yet.
	await act(async () => {
		view.queryClient.setQueryData(
			view.trpc.stream.isLive.queryKey({ broadcaster_id: "b1" }),
			false,
		);
		fireEvent.submit(
			screen
				.getByRole("button", { name: "videos.trigger_submit" })
				.closest("form") as HTMLFormElement,
		);
	});
	expect(
		(
			screen.getByRole("button", {
				name: "videos.trigger_submit",
			}) as HTMLButtonElement
		).disabled,
	).toBe(true);
	fireEvent.submit(
		screen
			.getByRole("button", { name: "videos.trigger_submit" })
			.closest("form") as HTMLFormElement,
	);
	expect(view.requests.some((path) => path.endsWith("triggerDownload"))).toBe(
		false,
	);
});

it("waits for a broadcaster check instead of interpreting unknown as offline", async () => {
	let finishLive!: (live: boolean) => void;
	const check = new Promise<boolean>((resolve) => {
		finishLive = resolve;
	});
	const view = harness(async () => check);
	await screen.findByText("videos.download.checking_live");
	expect(screen.queryByText("videos.download.offline")).toBeNull();
	const submit = screen.getByRole("button", {
		name: "videos.trigger_submit",
	}) as HTMLButtonElement;
	expect(submit.disabled).toBe(true);
	await act(async () => {
		fireEvent.submit(submit.closest("form") as HTMLFormElement);
	});
	expect(view.requests.some((path) => path.endsWith("triggerDownload"))).toBe(
		false,
	);
	finishLive(true);
	await waitFor(() => expect(submit.disabled).toBe(false));
	expect(view.requests.some((path) => path.endsWith("liveIds"))).toBe(false);
});

it("shows failed checks separately from offline and lets the user retry", async () => {
	const view = harness(async () => {
		throw new Error("Twitch unavailable");
	});
	await screen.findByText("videos.download.live_check_failed");
	expect(screen.queryByText("videos.download.offline")).toBeNull();
	view.setLookup(async () => true);
	fireEvent.click(
		screen.getByRole("button", { name: "videos.download.check_again" }),
	);
	await waitFor(() =>
		expect(
			(
				screen.getByRole("button", {
					name: "videos.trigger_submit",
				}) as HTMLButtonElement
			).disabled,
		).toBe(false),
	);
});

it("does not reuse a live verdict when the selected broadcaster changes", async () => {
	let finishSecond!: (live: boolean) => void;
	const second = new Promise<boolean>((resolve) => {
		finishSecond = resolve;
	});
	const view = harness(async (id) => (id === "b1" ? true : second));
	await waitFor(() =>
		expect(
			(
				screen.getByRole("button", {
					name: "videos.trigger_submit",
				}) as HTMLButtonElement
			).disabled,
		).toBe(false),
	);
	view.setBroadcaster("b2");
	const submit = screen.getByRole("button", {
		name: "videos.trigger_submit",
	}) as HTMLButtonElement;
	expect(submit.disabled).toBe(true);
	await act(async () => {
		fireEvent.submit(submit.closest("form") as HTMLFormElement);
	});
	finishSecond(false);
	await screen.findByText("videos.download.offline");
	expect(view.requests.some((path) => path.endsWith("triggerDownload"))).toBe(
		false,
	);
});
