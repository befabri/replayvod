import { expect, within } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { CHANNELS, makeChannelResponse, makeVideo } from "@/test/fixtures";
import { trpcParameters } from "@/test/trpc-mock";
import { VideoInfo } from "./VideoInfo";

const TAGGED = makeVideo(4);

const meta = preview.meta({
	title: "Features/Videos/VideoInfo",
	component: VideoInfo,
	args: { video: TAGGED },
	parameters: {
		layout: "padded",
		...trpcParameters({
			channel: {
				getById: ({ broadcaster_id }) =>
					makeChannelResponse(
						CHANNELS.findIndex((channel) => channel.id === broadcaster_id),
					),
			},
			video: {
				categories: () => [],
				statisticsByBroadcaster: () => ({
					total: 42,
					total_size: 84_000_000_000,
					total_duration_seconds: 302_400,
				}),
			},
			stream: {
				lastLive: ({ broadcaster_id }) => ({
					id: "last-live",
					broadcaster_id,
					type: "live",
					language: "en",
					viewer_count: 0,
					started_at: "2026-09-20T18:00:00Z",
					ended_at: "2026-09-20T23:00:00Z",
				}),
				liveIds: () => [],
			},
		}),
	},
	globals: { role: "viewer" },
});

export const Tagged = meta.story({
	play: async ({ canvas }) => {
		const list = canvas.getByRole("list", { name: i18n.t("videos.tags") });
		await expect(
			within(list)
				.getAllByRole("listitem")
				.map((item) => item.textContent),
		).toEqual(TAGGED.tags?.map((tag) => tag.name));
	},
});

export const Untagged = meta.story({
	args: { video: makeVideo(0) },
	play: async ({ canvas }) => {
		await expect(
			canvas.queryByRole("list", { name: i18n.t("videos.tags") }),
		).toBeNull();
	},
});
