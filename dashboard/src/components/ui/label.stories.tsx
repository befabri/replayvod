import preview from "#.storybook/preview";
import { CHANNELS } from "@/test/fixtures";
import { Checkbox } from "./checkbox";
import { Input } from "./input";
import { Label } from "./label";
import { Switch } from "./switch";

const meta = preview.meta({
	title: "UI/Label",
	component: Label,
	args: { children: "Channel" },
});

export const Default = meta.story();

export const ForInput = meta.story({
	render: (args) => (
		<div className="grid w-72 gap-1.5">
			<Label {...args} htmlFor="story-label-input" />
			<Input id="story-label-input" placeholder={CHANNELS[0].login} />
		</div>
	),
});

export const Muted = meta.story({
	args: { className: "text-muted-foreground" },
});

export const WrappingControl = meta.story({
	render: (args) => (
		<Label {...args} className="text-sm font-normal">
			<Checkbox defaultChecked />
			{CHANNELS[1].displayName}
		</Label>
	),
});

export const DisabledPeer = meta.story({
	render: (args) => (
		<div className="flex items-center gap-3">
			<Switch id="story-label-switch" disabled />
			<Label {...args} htmlFor="story-label-switch">
				{CHANNELS[2].displayName}
			</Label>
		</div>
	),
});
