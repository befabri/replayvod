import type { StoryContext } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { CHANNELS } from "@/test/fixtures";
import { effectiveOpacity } from "@/test/opacity";
import { Avatar } from "./avatar";
import { Label } from "./label";
import { Switch } from "./switch";

const CHANNEL = CHANNELS[0];

const meta = preview.meta({
	title: "UI/Switch",
	component: Switch,
	args: { id: "story-switch", onCheckedChange: fn() },
	render: (args) => (
		<div className="flex items-center gap-3">
			<Switch {...args} />
			<Label htmlFor={args.id}>{CHANNEL.displayName}</Label>
		</div>
	),
});

const expectDisabledAndDimmed = async ({
	canvas,
}: Pick<StoryContext, "canvas">) => {
	const control = canvas.getByRole("switch");
	await expect(control).toHaveAttribute("aria-disabled", "true");
	await expect(effectiveOpacity(control)).toBeLessThan(1);
	await expect(
		effectiveOpacity(canvas.getByText(CHANNEL.displayName)),
	).toBeLessThan(1);
};

export const Default = meta.story();

export const Checked = meta.story({
	args: { defaultChecked: true },
});

export const Disabled = meta.story({
	args: { disabled: true },
	play: expectDisabledAndDimmed,
});

export const DisabledChecked = meta.story({
	args: { disabled: true, defaultChecked: true },
	play: expectDisabledAndDimmed,
});

export const InRow = meta.story({
	args: { "aria-label": CHANNEL.displayName, defaultChecked: true },
	render: (args) => (
		<div className="flex w-64 items-center justify-between gap-3 rounded-md bg-popover px-3 py-2 text-sm text-popover-foreground">
			<span className="flex items-center gap-2">
				<Avatar
					src={CHANNEL.profileImageUrl}
					name={CHANNEL.displayName}
					size="sm"
				/>
				{CHANNEL.displayName}
			</span>
			<Switch {...args} />
		</div>
	),
});
