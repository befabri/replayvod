import { expect, within } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeWebhookDeliveries, makeWebhookDelivery } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { neverResolves, trpcParameters } from "@/test/trpc-mock";
import {
	RecordingWebhookDeliveries,
	RecordingWebhookDeliveriesSkeleton,
} from "./RecordingWebhookDeliveries";

const DELIVERIES = makeWebhookDeliveries(3);

const meta = preview.meta({
	title: "Features/RecordingWebhook/RecordingWebhookDeliveries",
	component: RecordingWebhookDeliveries,
	parameters: {
		layout: "padded",
		...trpcParameters({ recordingWebhook: { deliveries: () => DELIVERIES } }),
	},
});

export const Recent = meta.story({
	play: async ({ canvas }) => {
		await expect(await canvas.findAllByRole("listitem")).toHaveLength(3);
	},
});

export const WithFailures = meta.story({
	parameters: trpcParameters({
		recordingWebhook: {
			deliveries: () => [
				makeWebhookDelivery(0, {
					outcome: "failed",
					status: 502,
					attempts: 3,
					error: "bad gateway",
				}),
				makeWebhookDelivery(1, { outcome: "rejected", status: 410 }),
				makeWebhookDelivery(2, { test: true }),
			],
		},
	}),
});

export const Empty = meta.story({
	parameters: trpcParameters({ recordingWebhook: { deliveries: () => [] } }),
	play: async ({ canvas }) => {
		await expect(
			await canvas.findByText(i18n.t("webhook.deliveries_empty")),
		).toBeVisible();
	},
});

export const Loading = meta.story({
	parameters: trpcParameters({
		recordingWebhook: { deliveries: neverResolves },
	}),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
		await expect(
			canvas.getByText(i18n.t("webhook.deliveries_title")),
		).toBeVisible();
	},
});

// The page draws this card before the webhook config has loaded, and the
// card draws the same rows while its own list loads, so the recent deliveries
// replace the placeholders row for row.
export const SkeletonMatchesCard = meta.story({
	render: () => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<RecordingWebhookDeliveriesSkeleton />
			</div>
			<div data-testid="loaded">
				<RecordingWebhookDeliveries />
			</div>
		</div>
	),
	play: async ({ canvas }) => {
		const loaded = canvas.getByTestId("loaded");
		await expect(await within(loaded).findAllByRole("listitem")).toHaveLength(
			DELIVERIES.length,
		);
		await expect(
			layoutMismatches(canvas.getByTestId("skeleton"), loaded),
		).toEqual([]);
	},
});
