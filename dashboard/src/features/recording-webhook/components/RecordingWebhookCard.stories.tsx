import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeRecordingWebhookConfig } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import {
	RecordingWebhookCard,
	RecordingWebhookCardSkeleton,
} from "./RecordingWebhookCard";

const meta = preview.meta({
	title: "Features/RecordingWebhook/RecordingWebhookCard",
	component: RecordingWebhookCard,
	args: { data: makeRecordingWebhookConfig() },
	argTypes: { data: { control: false } },
	parameters: { layout: "padded" },
});

export const Configured = meta.story();

export const NeverEnabled = meta.story({
	args: {
		data: makeRecordingWebhookConfig({ enabled: false, url: "", secret: "" }),
	},
});

export const Loading = meta.story({
	render: () => <RecordingWebhookCardSkeleton />,
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

// The skeleton draws the configured card: once a webhook has been enabled it
// keeps a signing secret, so the secret row and its buttons are always there.
export const SkeletonMatchesCard = meta.story({
	render: (args) => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<RecordingWebhookCardSkeleton />
			</div>
			<div data-testid="loaded">
				<RecordingWebhookCard {...args} />
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
