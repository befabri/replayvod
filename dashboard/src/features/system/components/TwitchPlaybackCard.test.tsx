// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
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
