import type { StoryContext } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { CHANNELS } from "@/test/fixtures";
import { effectiveOpacity } from "@/test/opacity";
import { Checkbox } from "./checkbox";
import { Label } from "./label";

const meta = preview.meta({
	title: "UI/Checkbox",
	component: Checkbox,
	args: { id: "story-checkbox", onCheckedChange: fn() },
	render: (args) => (
		<div className="flex items-center gap-2">
			<Checkbox {...args} />
			<Label htmlFor={args.id} className="text-sm font-normal">
				{CHANNELS[0].displayName}
			</Label>
		</div>
	),
});

const expectDisabledAndDimmed = async ({
	canvas,
}: Pick<StoryContext, "canvas">) => {
	const control = canvas.getByRole("checkbox");
	await expect(control).toHaveAttribute("aria-disabled", "true");
	await expect(effectiveOpacity(control)).toBeLessThan(1);
	await expect(
		effectiveOpacity(canvas.getByText(CHANNELS[0].displayName)),
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

export const Group = meta.story({
	render: (args) => (
		<fieldset className="grid gap-2">
			<legend className="mb-1 text-sm font-medium">Channels</legend>
			{CHANNELS.slice(0, 4).map((channel, index) => (
				<div key={channel.id} className="flex items-center gap-2">
					<Checkbox
						{...args}
						id={`group-${channel.id}`}
						defaultChecked={index % 2 === 0}
					/>
					<Label htmlFor={`group-${channel.id}`} className="font-normal">
						{channel.displayName}
					</Label>
				</div>
			))}
		</fieldset>
	),
});

export const InsideDimmedContainer = meta.story({
	args: { disabled: true },
	render: (args) => (
		<div data-dimmed className="flex items-center gap-2 data-dimmed:opacity-50">
			<Checkbox {...args} />
			<Label htmlFor={args.id} className="text-sm font-normal">
				{CHANNELS[0].displayName}
			</Label>
		</div>
	),
	play: async ({ canvas }) => {
		await expect(effectiveOpacity(canvas.getByRole("checkbox"))).toBe(0.5);
		await expect(
			effectiveOpacity(canvas.getByText(CHANNELS[0].displayName)),
		).toBe(0.5);
	},
});
