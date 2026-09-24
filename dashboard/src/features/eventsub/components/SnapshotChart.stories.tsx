import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeSnapshots } from "@/test/fixtures";
import { SnapshotChart, SnapshotChartSkeleton } from "./SnapshotChart";

const meta = preview.meta({
	title: "Features/EventSub/SnapshotChart",
	component: SnapshotChart,
	args: { data: makeSnapshots(30) },
	argTypes: { data: { control: false } },
	parameters: { layout: "padded" },
});

export const Default = meta.story();

export const Skeleton = meta.story({
	render: () => <SnapshotChartSkeleton />,
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

// The chart shows no fixed text, so its skeleton holds the bars and the date
// range line at the same heights.
export const SkeletonMatchesChart = meta.story({
	render: (args) => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<SnapshotChartSkeleton />
			</div>
			<div data-testid="loaded">
				<SnapshotChart {...args} />
			</div>
		</div>
	),
	play: async ({ canvas }) => {
		const height = (id: string) =>
			Math.round(canvas.getByTestId(id).getBoundingClientRect().height);
		await expect(height("skeleton")).toBe(height("loaded"));
	},
});
