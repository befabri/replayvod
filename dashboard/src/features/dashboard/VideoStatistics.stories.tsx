import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeVideoStatistics } from "@/test/fixtures";
import { neverResolves, trpcParameters } from "@/test/trpc-mock";
import { VideoStatistics } from "./VideoStatistics";

const STATISTICS = makeVideoStatistics();

const meta = preview.meta({
	title: "Features/Dashboard/VideoStatistics",
	component: VideoStatistics,
	parameters: {
		layout: "padded",
		...trpcParameters({ video: { statistics: () => STATISTICS } }),
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
		for (const bucket of STATISTICS.by_status) {
			await expect(
				await canvas.findByText(bucket.count.toLocaleString()),
			).toBeVisible();
		}
	},
});

export const Loading = meta.story({
	parameters: trpcParameters({ video: { statistics: neverResolves } }),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
		await expect(canvas.getByText(i18n.t("videos.status.DONE"))).toBeVisible();
	},
});
