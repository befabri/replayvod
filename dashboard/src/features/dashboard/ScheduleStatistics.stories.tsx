import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { CHANNELS, makeChannelResponse, makeSchedules } from "@/test/fixtures";
import { neverResolves, trpcParameters } from "@/test/trpc-mock";
import { ScheduleStatistics } from "./ScheduleStatistics";

const SCHEDULES = makeSchedules(5);

const meta = preview.meta({
	title: "Features/Dashboard/ScheduleStatistics",
	component: ScheduleStatistics,
	parameters: {
		layout: "padded",
		...trpcParameters({
			schedule: { mine: () => ({ data: SCHEDULES }) },
			channel: {
				getById: ({ broadcaster_id }) =>
					makeChannelResponse(
						CHANNELS.findIndex((channel) => channel.id === broadcaster_id),
					),
			},
		}),
	},
	globals: { role: "owner" },
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
			await canvas.findByRole("link", { name: CHANNELS[0].displayName }),
		).toBeVisible();
	},
});

export const Loading = meta.story({
	parameters: trpcParameters({ schedule: { mine: neverResolves } }),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});
