import { expect, within } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeSnapshot } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import { neverResolves, trpcParameters } from "@/test/trpc-mock";
import { QuotaCard, QuotaCardSkeleton } from "./QuotaCard";

const SNAPSHOT = makeSnapshot();

const meta = preview.meta({
	title: "Features/EventSub/QuotaCard",
	component: QuotaCard,
	parameters: {
		layout: "padded",
		...trpcParameters({
			eventsub: { latestSnapshot: () => ({ snapshot: SNAPSHOT }) },
		}),
	},
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		await expect(await canvas.findByText(String(SNAPSHOT.total))).toBeVisible();
	},
});

export const NoSnapshot = meta.story({
	parameters: trpcParameters({ eventsub: { latestSnapshot: () => ({}) } }),
	play: async ({ canvas }) => {
		await expect(
			await canvas.findByText(i18n.t("eventsub.no_snapshot_yet")),
		).toBeVisible();
	},
});

export const Loading = meta.story({
	parameters: trpcParameters({ eventsub: { latestSnapshot: neverResolves } }),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

export const SkeletonMatchesCard = meta.story({
	render: () => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<QuotaCardSkeleton />
			</div>
			<div data-testid="loaded">
				<QuotaCard />
			</div>
		</div>
	),
	play: async ({ canvas }) => {
		const loaded = canvas.getByTestId("loaded");
		await expect(
			await within(loaded).findByText(String(SNAPSHOT.total)),
		).toBeVisible();
		await expect(
			layoutMismatches(canvas.getByTestId("skeleton"), loaded),
		).toEqual([]);
	},
});
