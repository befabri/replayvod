import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeEventSubConfig } from "@/test/fixtures";
import { layoutMismatches } from "@/test/layout";
import {
	EventSubSetupCard,
	EventSubSetupCardSkeleton,
} from "./EventSubSetupCard";

const meta = preview.meta({
	title: "Features/EventSub/EventSubSetupCard",
	component: EventSubSetupCard,
	args: { data: makeEventSubConfig() },
	argTypes: { data: { control: false } },
	parameters: { layout: "padded" },
});

export const Poll = meta.story();

export const Direct = meta.story({
	args: { data: makeEventSubConfig({ mode: "direct" }) },
});

export const RestartRequired = meta.story({
	args: {
		data: makeEventSubConfig({ mode: "direct", restart_required: true }),
	},
});

export const EnvManaged = meta.story({
	args: { data: makeEventSubConfig({ env_managed: true }) },
});

export const Loading = meta.story({
	render: () => <EventSubSetupCardSkeleton />,
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
		await expect(canvas.getByText(i18n.t("eventsub.mode_poll"))).toBeVisible();
	},
});

// The mode options are fixed text, so the skeleton draws them for real and
// only holds the radios, the status badge and the save button. It matches a
// mode with no extra field below the options, which polling, the default, is.
export const SkeletonMatchesCard = meta.story({
	render: (args) => (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<EventSubSetupCardSkeleton />
			</div>
			<div data-testid="loaded">
				<EventSubSetupCard {...args} />
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
