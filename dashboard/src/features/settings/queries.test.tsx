// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import type { ReactNode } from "react";
import { afterEach, expect, it } from "vitest";
import type { SettingsResponse } from "@/api/generated/trpc";
import { type AppRouter, TRPCProvider, useTRPC } from "@/api/trpc";
import { type AuthUser, authStore } from "@/stores/auth";
import { USER_SETTINGS } from "@/test/playback-settings";
import {
	useKeepSettingsLoaded,
	useSettings,
	useUpdatePlaybackSettings,
	useUpdateSettings,
} from "./queries";

afterEach(cleanup);

function deferred<T>() {
	let resolve!: (value: T) => void;
	const promise = new Promise<T>((complete) => {
		resolve = complete;
	});
	return { promise, resolve };
}

function providers(
	fetch: (procedure: string) => Promise<SettingsResponse>,
	gcTime?: number,
) {
	authStore.setState((state) => ({
		...state,
		user: { id: USER_SETTINGS.user_id } as AuthUser,
	}));
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: { retry: false, ...(gcTime == null ? {} : { gcTime }) },
			mutations: { retry: false },
		},
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: async (url) =>
					new Response(
						JSON.stringify({
							result: {
								data: await fetch(
									new URL(String(url)).pathname.split("/").at(-1) ?? "",
								),
							},
						}),
						{ headers: { "Content-Type": "application/json" } },
					),
			}),
		],
	});
	const wrapper = ({ children }: { children: ReactNode }) => (
		<QueryClientProvider client={queryClient}>
			<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
				{children}
			</TRPCProvider>
		</QueryClientProvider>
	);
	return { queryClient, wrapper };
}

function harness(fetch: (procedure: string) => Promise<SettingsResponse>) {
	const { queryClient, wrapper } = providers(fetch);
	const { result } = renderHook(
		() => {
			const trpc = useTRPC();
			return {
				trpc,
				settings: useSettings(),
				playback: useUpdatePlaybackSettings(),
				general: useUpdateSettings(),
			};
		},
		{ wrapper },
	);
	return { queryClient, result };
}

it("rejects a settings snapshot started while playback preferences were saving", async () => {
	const save = deferred<SettingsResponse>();
	const snapshot = deferred<SettingsResponse>();
	let reads = 0;
	const { queryClient, result } = harness(async (procedure) => {
		if (procedure === "settings.updatePlayback") return save.promise;
		return ++reads === 1 ? USER_SETTINGS : snapshot.promise;
	});
	await waitFor(() => expect(result.current.settings.data).toBeDefined());
	const playback = { ...USER_SETTINGS.playback, resume_min_seconds: 20 };
	let saving!: Promise<SettingsResponse>;
	act(() => {
		saving = result.current.playback.mutateAsync(playback);
	});
	await waitFor(() => expect(result.current.playback.isPending).toBe(true));
	act(() => {
		void queryClient.refetchQueries({
			queryKey: result.current.trpc.settings.get.pathKey(),
		});
	});
	await waitFor(() => expect(reads).toBe(2));
	await act(async () => {
		save.resolve({ ...USER_SETTINGS, playback });
		await saving;
	});
	expect(
		queryClient.getQueryData(result.current.trpc.settings.get.queryKey())
			?.playback,
	).toEqual(playback);
	await act(async () => {
		snapshot.resolve(USER_SETTINGS);
		await snapshot.promise;
	});
	expect(
		queryClient.getQueryData(result.current.trpc.settings.get.queryKey())
			?.playback,
	).toEqual(playback);
});

it("keeps newer locale fields when a playback response carries an older settings row", async () => {
	const save = deferred<SettingsResponse>();
	const locale = { ...USER_SETTINGS, timezone: "Europe/Brussels" };
	let server = USER_SETTINGS;
	const { result, queryClient } = harness(async (procedure) => {
		if (procedure === "settings.updatePlayback") return save.promise;
		if (procedure === "settings.update") {
			server = locale;
			return server;
		}
		return server;
	});
	await waitFor(() => expect(result.current.settings.data).toBeDefined());
	const playback = { ...USER_SETTINGS.playback, resume_min_seconds: 20 };
	let saving!: Promise<SettingsResponse>;
	act(() => {
		saving = result.current.playback.mutateAsync(playback);
	});
	await act(async () => {
		await result.current.general.mutateAsync({
			timezone: locale.timezone,
			datetime_format: "ISO",
			language: "en",
		});
	});
	await waitFor(() =>
		expect(result.current.settings.data?.timezone).toBe(locale.timezone),
	);
	await act(async () => {
		save.resolve({ ...USER_SETTINGS, playback });
		await saving;
	});
	expect(
		queryClient.getQueryData(result.current.trpc.settings.get.queryKey())
			?.timezone,
	).toBe(locale.timezone);
	expect(
		queryClient.getQueryData(result.current.trpc.settings.get.queryKey())
			?.playback,
	).toEqual(playback);
});

it("does not publish a settings save after the account changes", async () => {
	const save = deferred<SettingsResponse>();
	const { result, queryClient } = harness(async (procedure) =>
		procedure === "settings.updatePlayback" ? save.promise : USER_SETTINGS,
	);
	await waitFor(() => expect(result.current.settings.data).toBeDefined());
	const playback = { ...USER_SETTINGS.playback, resume_min_seconds: 20 };
	let saving!: Promise<SettingsResponse>;
	act(() => {
		saving = result.current.playback.mutateAsync(playback);
	});
	await waitFor(() => expect(result.current.playback.isPending).toBe(true));
	const nextAccount = { ...USER_SETTINGS, user_id: "u2" };
	act(() => {
		authStore.setState((state) => ({
			...state,
			user: { id: nextAccount.user_id } as AuthUser,
		}));
		queryClient.setQueryData(
			result.current.trpc.settings.get.queryKey(),
			nextAccount,
		);
	});
	await act(async () => {
		save.resolve({ ...USER_SETTINGS, playback });
		await saving;
	});
	expect(
		queryClient.getQueryData(result.current.trpc.settings.get.queryKey()),
	).toEqual(nextAccount);
});

// Settings are read through Suspense by pages the user opens much later. If
// the layout only prefetched them, nothing would observe the query on pages
// without video consumers, it would be garbage collected, and the next video
// page would fall back to its skeleton while settings load again.
it("keeps settings cached while the dashboard layout is mounted", async () => {
	const { queryClient, wrapper } = providers(async () => USER_SETTINGS, 5);
	const { result } = renderHook(
		() => {
			useKeepSettingsLoaded();
			return useTRPC();
		},
		{ wrapper },
	);
	const key = result.current.settings.get.queryKey();
	await waitFor(() => expect(queryClient.getQueryData(key)).toBeDefined());

	await new Promise((resolve) => setTimeout(resolve, 50));

	expect(queryClient.getQueryData(key)).toEqual(USER_SETTINGS);
});

it("labels a settings failure as a settings failure", async () => {
	const { queryClient, wrapper } = providers(async () => USER_SETTINGS);
	const { result } = renderHook(() => useTRPC(), { wrapper });
	const key = result.current.settings.get.queryKey();
	renderHook(() => useSettings(), { wrapper });
	await waitFor(() => expect(queryClient.getQueryData(key)).toBeDefined());

	expect(queryClient.getQueryCache().find({ queryKey: key })?.meta).toEqual({
		errorLabel: "settings.failed_to_load",
	});
});
