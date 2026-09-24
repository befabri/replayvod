import preview from "#.storybook/preview";
import { VOD_TITLES } from "@/test/fixtures";
import { ExpandableText } from "./expandable-text";

const LONG_DESCRIPTION = `${VOD_TITLES.join(". ")}. ${VOD_TITLES.join(". ")}.`;

const meta = preview.meta({
	title: "UI/ExpandableText",
	component: ExpandableText,
	args: {
		children: LONG_DESCRIPTION,
		className: "text-sm leading-6 text-muted-foreground",
	},
	decorators: [
		(Story) => (
			<div className="max-w-md">
				<Story />
			</div>
		),
	],
});

export const Long = meta.story();

export const Short = meta.story({
	args: {
		children: VOD_TITLES[0],
	},
});
