import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makePlaybackCacheConfig } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import {
	PlaybackCacheCard,
	PlaybackCacheCardSkeleton,
} from "./PlaybackCacheCard";

const meta = preview.meta({
	title: "Features/System/PlaybackCacheCard",
	component: PlaybackCacheCard,
	args: { data: makePlaybackCacheConfig() },
	argTypes: { data: { control: false } },
	parameters: { layout: "padded" },
});

export const Enabled = meta.story();

export const Disabled = meta.story({
	args: { data: makePlaybackCacheConfig({ enabled: false }) },
});

export const Loading = meta.story({
	render: () => <PlaybackCacheCardSkeleton />,
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

// The skeleton draws the card's own title, labels and hints, and holds each
// control's place, so nothing moves when the settings arrive.
export const SkeletonMatchesCard = meta.story({
	render: (args) => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<PlaybackCacheCardSkeleton />
			</div>
			<div data-testid="loaded">
				<PlaybackCacheCard {...args} />
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
