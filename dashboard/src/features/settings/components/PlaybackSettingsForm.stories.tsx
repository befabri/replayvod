import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { USER_SETTINGS } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import {
	PlaybackSettingsForm,
	PlaybackSettingsFormSkeleton,
} from "./PlaybackSettingsForm";

const meta = preview.meta({
	title: "Features/Settings/PlaybackSettingsForm",
	component: PlaybackSettingsForm,
	args: { data: USER_SETTINGS },
	argTypes: { data: { control: false } },
	parameters: { layout: "padded" },
});

export const Default = meta.story();

export const Loading = meta.story({
	render: () => <PlaybackSettingsFormSkeleton />,
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

// The skeleton draws the section's heading, labels and hints and holds each
// field's place, so nothing moves when the settings arrive.
export const SkeletonMatchesCard = meta.story({
	render: (args) => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<PlaybackSettingsFormSkeleton />
			</div>
			<div data-testid="loaded">
				<PlaybackSettingsForm {...args} />
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
