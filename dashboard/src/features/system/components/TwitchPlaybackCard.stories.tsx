import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import { TRPCProvider } from "@/api/trpc";
import i18n from "@/i18n";
import { makeTwitchPlaybackStatus } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import {
	createMockTrpcClient,
	neverResolves,
	type TrpcHandlers,
	trpcParameters,
} from "@/test/trpc-mock";
import { TwitchPlaybackCard } from "./TwitchPlaybackCard";

const meta = preview.meta({
	title: "Features/System/TwitchPlaybackCard",
	component: TwitchPlaybackCard,
	parameters: {
		layout: "padded",
		...trpcParameters({
			twitchPlayback: { status: () => makeTwitchPlaybackStatus() },
		}),
	},
});

export const Connected = meta.story();

export const Anonymous = meta.story({
	parameters: trpcParameters({
		twitchPlayback: {
			status: () => makeTwitchPlaybackStatus("disconnected"),
		},
	}),
});

export const Loading = meta.story({
	parameters: trpcParameters({ twitchPlayback: { status: neverResolves } }),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

// Each card needs its own cache: one is still waiting for its status while
// the other already has it.
function OwnCache({
	handlers,
	children,
}: {
	handlers: TrpcHandlers;
	children: ReactNode;
}) {
	const [queryClient] = useState(
		() => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
	);
	const [trpcClient] = useState(() => createMockTrpcClient(handlers));
	return (
		<QueryClientProvider client={queryClient}>
			<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
				{children}
			</TRPCProvider>
		</QueryClientProvider>
	);
}

// Only the status row waits for data. It keeps the height of its tallest
// state, so the guide and the form below stay put whatever the status turns
// out to be.
export const SkeletonMatchesCard = meta.story({
	render: () => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<OwnCache handlers={{ twitchPlayback: { status: neverResolves } }}>
					<TwitchPlaybackCard />
				</OwnCache>
			</div>
			<div data-testid="connected">
				<OwnCache
					handlers={{
						twitchPlayback: { status: () => makeTwitchPlaybackStatus() },
					}}
				>
					<TwitchPlaybackCard />
				</OwnCache>
			</div>
			<div data-testid="anonymous">
				<OwnCache
					handlers={{
						twitchPlayback: {
							status: () => makeTwitchPlaybackStatus("disconnected"),
						},
					}}
				>
					<TwitchPlaybackCard />
				</OwnCache>
			</div>
		</div>
	),
	play: async ({ canvas }) => {
		await expect(
			await canvas.findByText(i18n.t("twitch_playback.connected")),
		).toBeVisible();
		await expect(
			await canvas.findByText(i18n.t("twitch_playback.disconnected")),
		).toBeVisible();
		for (const loaded of ["connected", "anonymous"]) {
			await expect(
				layoutMismatches(
					canvas.getByTestId("skeleton"),
					canvas.getByTestId(loaded),
				),
			).toEqual([]);
		}
	},
});
