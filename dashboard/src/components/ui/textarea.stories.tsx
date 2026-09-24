import { fn } from "storybook/test";
import preview from "#.storybook/preview";
import { VOD_TITLES } from "@/test/fixtures";
import { Label } from "./label";
import { Textarea } from "./textarea";

const meta = preview.meta({
	title: "UI/Textarea",
	component: Textarea,
	args: {
		rows: 4,
		placeholder: VOD_TITLES[0],
		onChange: fn(),
		"aria-label": "Notes",
	},
	decorators: [
		(Story) => (
			<div className="w-96">
				<Story />
			</div>
		),
	],
});

export const Default = meta.story();

export const WithLabel = meta.story({
	args: { "aria-label": undefined },
	render: (args) => (
		<div className="grid gap-1.5">
			<Label htmlFor="story-textarea">Notes</Label>
			<Textarea {...args} id="story-textarea" />
		</div>
	),
});

export const Filled = meta.story({
	args: { defaultValue: VOD_TITLES.slice(0, 4).join("\n") },
});

export const Disabled = meta.story({
	args: { disabled: true, defaultValue: VOD_TITLES[1] },
});

export const Invalid = meta.story({
	args: { "aria-invalid": true, defaultValue: VOD_TITLES[2] },
});
