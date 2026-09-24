import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeFollowedStreams } from "@/test/fixtures";
import { neverResolves, trpcParameters } from "@/test/trpc-mock";
import { LastLiveStatistics } from "./LastLiveStatistics";

const STREAMS = makeFollowedStreams(6);

const meta = preview.meta({
	title: "Features/Dashboard/LastLiveStatistics",
	component: LastLiveStatistics,
	parameters: {
		layout: "padded",
		...trpcParameters({ stream: { followed: () => STREAMS } }),
	},
	decorators: [
		(Story) => (
			<div className="max-w-md">
				<Story />
			</div>
		),
	],
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		await expect(
			await canvas.findByRole("link", { name: STREAMS[0].broadcaster_name }),
		).toBeVisible();
	},
});

export const Empty = meta.story({
	parameters: trpcParameters({ stream: { followed: () => [] } }),
	play: async ({ canvas }) => {
		await expect(
			await canvas.findByText(i18n.t("streams_live.empty")),
		).toBeVisible();
	},
});

export const Loading = meta.story({
	parameters: trpcParameters({ stream: { followed: neverResolves } }),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});
