// @vitest-environment jsdom
import {
	QueryClient,
	QueryClientProvider,
	useQuery,
} from "@tanstack/react-query";
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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LiveRenditionsResponse } from "@/api/generated/trpc";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { liveRenditionsOptions } from "@/features/videos/queries";
import { TwitchPlaybackCard } from "./TwitchPlaybackCard";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/env", () => ({ API_URL: "" }));
beforeEach(() => vi.stubGlobal("isSecureContext", true));
afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

it.each([
	"connect",
	"check",
	"disconnect",
] as const)("%s resets active and inactive renditions and rejects responses from the old session", async (action) => {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false, gcTime: Infinity } },
	});
	const previous: LiveRenditionsResponse = {
		anonymous: action === "connect",
		renditions: [{ height: action === "connect" ? 1080 : 1440, codec: "h265" }],
	};
	const next: LiveRenditionsResponse = {
		anonymous: action !== "connect",
		renditions: [{ height: action === "connect" ? 1440 : 1080, codec: "h265" }],
	};
	let changed = false;
	let finishOld!: (response: Response) => void;
	const oldResponse = new Promise<Response>((resolve) => {
		finishOld = resolve;
	});
	let renditionCalls = 0;
	const json = (data: unknown) =>
		new Response(JSON.stringify({ result: { data } }), {
			headers: { "Content-Type": "application/json" },
		});
	const client = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "https://server.example/trpc",
				fetch: async (url) => {
					const proc = new URL(String(url)).pathname;
					if (proc.endsWith("video.liveRenditions")) {
						renditionCalls++;
						return changed ? json(next) : oldResponse;
					}
					if (proc.endsWith(`twitchPlayback.${action}`)) changed = true;
					return json({
						state: changed
							? action === "connect"
								? "connected"
								: action === "check"
									? "reconnect_required"
									: "disconnected"
							: action === "connect"
								? "disconnected"
								: "connected",
						login: "viewer",
						checked_at: 0,
						expires_at: 0,
					});
				},
			}),
		],
	});
	const trpc = createTRPCOptionsProxy<AppRouter>({ client, queryClient });
	const active = liveRenditionsOptions(trpc, "b1", false);
	const inactive = [
		liveRenditionsOptions(trpc, "b1", true),
		liveRenditionsOptions(trpc, "b2", false),
	];
	for (const options of [active, ...inactive])
		queryClient.setQueryData(options.queryKey, previous);
	queryClient.setQueryData(["unrelated"], "keep");
	function Qualities() {
		const { data } = useQuery(active);
		return <output data-testid="qualities">{JSON.stringify(data)}</output>;
	}
	render(
		<QueryClientProvider client={queryClient}>
			<TRPCProvider trpcClient={client} queryClient={queryClient}>
				<TwitchPlaybackCard />
				<Qualities />
			</TRPCProvider>
		</QueryClientProvider>,
	);
	await screen.findByText(
		`twitch_playback.${action === "connect" ? "disconnected" : "connected"}`,
	);
	let staleRequest!: Promise<void>;
	act(() => {
		staleRequest = queryClient.refetchQueries({ queryKey: active.queryKey });
	});
	await waitFor(() => expect(renditionCalls).toBe(1));
	if (action === "connect") {
		fireEvent.change(screen.getByLabelText("twitch_playback.token_label"), {
			target: { value: "test-session-0123456789abcdef" },
		});
		fireEvent.click(screen.getByRole("checkbox"));
	}
	fireEvent.click(
		screen.getByRole("button", { name: `twitch_playback.${action}` }),
	);
	await screen.findByText(`twitch_playback.${action}_success`);
	await waitFor(() =>
		expect(screen.getByTestId("qualities").textContent).toBe(
			JSON.stringify(next),
		),
	);
	for (const options of inactive)
		expect(queryClient.getQueryData(options.queryKey)).toBeUndefined();
	await act(async () => {
		finishOld(json(previous));
		await staleRequest;
	});
	expect(queryClient.getQueryData(active.queryKey)).toEqual(next);
	for (const options of inactive)
		expect(await queryClient.ensureQueryData(options)).toEqual(next);
	expect(queryClient.getQueryData(["unrelated"])).toBe("keep");
	queryClient.clear();
});

describe("Twitch playback connection", () => {
	it.each([
		false,
		true,
	])("requires consent and clears credentials without caching them (failure=%s)", async (fail) => {
		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false, gcTime: 0 } },
		});
		const token = "private-session-0123456789abcdef";
		const calls: string[] = [];
		const trpcClient = createTRPCClient<AppRouter>({
			links: [
				httpLink({
					url: "https://server.example/trpc",
					fetch: async (url, options) => {
						expect(String(url)).not.toContain(token);
						const proc = new URL(String(url)).pathname;
						calls.push(proc);
						if (proc.endsWith(".connect")) {
							expect(options?.method).toBe("POST");
							expect(JSON.parse(String(options?.body))).toEqual({
								session_token: token,
								consent: true,
							});
							if (fail) throw new Error("Twitch unavailable");
						}
						return new Response(
							JSON.stringify({
								result: {
									data: {
										state: proc.endsWith(".connect")
											? "connected"
											: "disconnected",
										login: proc.endsWith(".connect") ? "viewer" : "",
										checked_at: 0,
										expires_at: 0,
									},
								},
							}),
							{ headers: { "Content-Type": "application/json" } },
						);
					},
				}),
			],
		});
		render(
			<QueryClientProvider client={queryClient}>
				<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
					<TwitchPlaybackCard />
				</TRPCProvider>
			</QueryClientProvider>,
		);
		await screen.findByText("twitch_playback.disconnected");
		const field = screen.getByLabelText(
			"twitch_playback.token_label",
		) as HTMLInputElement;
		expect(field.type).toBe("password");
		fireEvent.change(field, { target: { value: token } });
		const submit = screen.getByRole("button", {
			name: "twitch_playback.connect",
		}) as HTMLButtonElement;
		expect(submit.disabled).toBe(true);
		fireEvent.submit(field.closest("form") as HTMLFormElement);
		expect(calls.filter((p) => p.endsWith(".connect"))).toHaveLength(0);
		fireEvent.click(screen.getByRole("checkbox"));
		fireEvent.click(submit);
		expect(field.value).toBe("");
		await waitFor(() =>
			expect(calls.filter((p) => p.endsWith(".connect"))).toHaveLength(1),
		);
		if (fail) await screen.findByRole("alert");
		else await screen.findByText("viewer");
		expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
		expect(
			JSON.stringify(
				queryClient
					.getQueryCache()
					.getAll()
					.map((q) => q.state),
			),
		).not.toContain(token);
		if (!fail) {
			fireEvent.click(
				screen.getByRole("button", { name: "twitch_playback.disconnect" }),
			);
			await screen.findByText("twitch_playback.disconnected");
			expect(screen.queryByText("viewer")).toBeNull();
		}
		queryClient.clear();
	});
});
