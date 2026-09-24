import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { channelAt, makeChannelResponse, makeSchedule } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { trpcParameters } from "@/test/trpc-mock";
import { ScheduleRow, ScheduleRowSkeleton } from "./ScheduleRow";

const meta = preview.meta({
	title: "Features/Schedules/ScheduleRow",
	component: ScheduleRow,
	args: { schedule: makeSchedule(1), canManage: true },
	argTypes: { schedule: { control: false } },
	parameters: {
		layout: "padded",
		...trpcParameters({ channel: { getById: () => makeChannelResponse(1) } }),
	},
	decorators: [
		(Story) => (
			<div className="max-w-2xl">
				<Story />
			</div>
		),
	],
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		await expect(
			await canvas.findByText(channelAt(1).displayName),
		).toBeVisible();
	},
});

export const Viewer = meta.story({
	args: { canManage: false },
});

export const Skeleton = meta.story({
	render: (args) => <ScheduleRowSkeleton canManage={args.canManage} />,
	play: async ({ canvas }) => {
		await expect(
			canvas.queryByRole("button", { name: i18n.t("schedules.edit") }),
		).toBeNull();
	},
});

// The skeleton keeps the row's own Edit column and holds each line's place,
// so the schedules grid does not move when the rows arrive.
export const SkeletonMatchesRow = meta.story({
	render: (args) => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<ScheduleRowSkeleton canManage={args.canManage} />
			</div>
			<div data-testid="loaded">
				<ScheduleRow {...args} />
			</div>
		</div>
	),
	play: async ({ canvas }) => {
		await canvas.findByText(channelAt(1).displayName);
		await expect(
			layoutMismatches(
				canvas.getByTestId("skeleton"),
				canvas.getByTestId("loaded"),
			),
		).toEqual([]);
	},
});
