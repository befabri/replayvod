import { fn } from "storybook/test";
import preview from "#.storybook/preview";
import { Button } from "./button";
import { EmptyPanel } from "./empty-panel";

const meta = preview.meta({
	title: "UI/EmptyPanel",
	component: EmptyPanel,
	args: { children: "Nothing here yet." },
});

export const Default = meta.story();

export const WithAction = meta.story({
	args: {
		children: (
			<div className="flex flex-col items-center gap-3">
				<p>Nothing here yet.</p>
				<Button variant="outline" size="sm" onClick={fn()}>
					Add
				</Button>
			</div>
		),
	},
});
