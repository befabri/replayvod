import { fn } from "storybook/test";
import preview from "#.storybook/preview";
import { CHANNELS } from "@/test/fixtures";
import { Input } from "./input";
import { Label } from "./label";

const CHANNEL = CHANNELS[0];

const meta = preview.meta({
	title: "UI/Input",
	component: Input,
	args: {
		type: "text",
		placeholder: CHANNEL.login,
		onChange: fn(),
		"aria-label": "Channel",
	},
	argTypes: {
		type: {
			control: "select",
			options: ["text", "url", "search", "number", "password", "file"],
		},
	},
	decorators: [
		(Story) => (
			<div className="w-80">
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
			<Label htmlFor="story-input">Channel</Label>
			<Input {...args} id="story-input" />
			<span className="text-xs text-muted-foreground">Helper text</span>
		</div>
	),
});

export const Filled = meta.story({
	args: { defaultValue: CHANNEL.login },
});

export const Disabled = meta.story({
	args: { disabled: true, defaultValue: CHANNEL.login },
});

export const Invalid = meta.story({
	args: { "aria-invalid": true, defaultValue: `@${CHANNEL.login}!` },
	render: (args) => (
		<div className="grid gap-1.5">
			<Input {...args} />
			<span className="text-xs text-destructive">Error message</span>
		</div>
	),
});

export const Types = meta.story({
	args: { "aria-label": undefined },
	render: (args) => (
		<div className="grid gap-3">
			<Input
				{...args}
				type="search"
				placeholder="Search…"
				aria-label="Search"
			/>
			<Input
				{...args}
				type="number"
				placeholder="0"
				defaultValue={250}
				min={0}
				aria-label="Number"
			/>
			<Input
				{...args}
				type="password"
				defaultValue="hunter2"
				aria-label="Password"
			/>
			<Input {...args} type="file" placeholder="" aria-label="File" />
		</div>
	),
});
