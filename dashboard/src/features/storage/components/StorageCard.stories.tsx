import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeStorageDetails } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { StorageCard, StorageCardSkeleton } from "./StorageCard";

const meta = preview.meta({
	title: "Features/Storage/StorageCard",
	component: StorageCard,
	args: { data: makeStorageDetails() },
	argTypes: { data: { control: false } },
	parameters: { layout: "padded" },
	decorators: [
		(Story) => (
			<div className="max-w-3xl">
				<Story />
			</div>
		),
	],
});

export const Attached = meta.story();

export const Skeleton = meta.story({
	render: () => <StorageCardSkeleton />,
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
		await expect(canvas.getByText(i18n.t("storage.card_title"))).toBeVisible();
	},
});

// The skeleton draws the card's own labels and holds each value's line, so
// nothing moves when the storage check arrives.
export const SkeletonMatchesCard = meta.story({
	render: (args) => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<StorageCardSkeleton />
			</div>
			<div data-testid="loaded">
				<StorageCard {...args} />
			</div>
		</div>
	),
	play: async ({ canvas }) => {
		await expect(
			layoutMismatches(
				canvas.getByTestId("skeleton"),
				canvas.getByTestId("loaded"),
			),
		).toEqual([]);
	},
});
