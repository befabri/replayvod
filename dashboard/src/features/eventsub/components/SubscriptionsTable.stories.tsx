import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeSubscriptions } from "@/test/fixtures";
import { SubscriptionsTable } from "./SubscriptionsTable";

const LOADING_ROWS = 5;

const meta = preview.meta({
	title: "Features/EventSub/SubscriptionsTable",
	component: SubscriptionsTable,
	args: { rows: makeSubscriptions(LOADING_ROWS) },
	argTypes: { rows: { control: false } },
	parameters: { layout: "padded" },
});

export const Active = meta.story();

export const Empty = meta.story({
	args: { rows: [] },
	play: async ({ canvas }) => {
		await expect(
			canvas.getByText(i18n.t("eventsub.no_subscriptions")),
		).toBeVisible();
	},
});

export const Loading = meta.story({
	args: { rows: [], loading: true },
	play: async ({ canvas }) => {
		await expect(canvas.getByRole("table")).toHaveAttribute(
			"aria-busy",
			"true",
		);
	},
});

// The loading table draws as many rows as the fixture holds, so the check
// proves each placeholder row is as tall as a real subscription row. Columns
// size to their content, so only the vertical layout can match.
export const SkeletonMatchesTable = meta.story({
	render: (args) => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<SubscriptionsTable rows={[]} loading />
			</div>
			<div data-testid="loaded">
				<SubscriptionsTable {...args} />
			</div>
		</div>
	),
	play: async ({ canvas }) => {
		const rowHeights = (id: string) =>
			[...canvas.getByTestId(id).querySelectorAll("tr")].map((row) =>
				Math.round(row.getBoundingClientRect().height),
			);
		await expect(rowHeights("skeleton")).toEqual(rowHeights("loaded"));
		await expect(rowHeights("loaded")).toHaveLength(LOADING_ROWS + 1);
	},
});
