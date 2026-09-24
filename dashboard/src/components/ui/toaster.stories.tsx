import { expect, fn, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import { CHANNELS, VOD_TITLES } from "@/test/fixtures";
import { Button } from "./button";
import { Toaster, toast } from "./toaster";

const meta = preview.meta({
	title: "UI/Toaster",
	component: Toaster,
	parameters: {
		docs: {
			description: {
				component:
					"Storybook mounts one Toaster for every story, the way the app root does, so these buttons toast into it.",
			},
		},
	},
});

const undo = fn();

export const Kinds = meta.story({
	render: () => (
		<div className="flex flex-wrap gap-2">
			<Button
				variant="outline"
				onClick={() => toast.success(CHANNELS[0].displayName)}
			>
				Success
			</Button>
			<Button variant="outline" onClick={() => toast.error(VOD_TITLES[1])}>
				Error
			</Button>
			<Button
				variant="outline"
				onClick={() =>
					toast.warning(CHANNELS[1].displayName, {
						description: VOD_TITLES[2],
					})
				}
			>
				Warning
			</Button>
			<Button variant="outline" onClick={() => toast.info(VOD_TITLES[3])}>
				Info
			</Button>
			<Button
				variant="outline"
				onClick={() =>
					toast(VOD_TITLES[0], {
						description: CHANNELS[2].displayName,
						action: { label: "Undo", onClick: undo },
					})
				}
			>
				With action
			</Button>
		</div>
	),
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(canvas.getByRole("button", { name: "Success" }));
		await userEvent.click(canvas.getByRole("button", { name: "With action" }));
		await waitFor(() =>
			expect(screen.getByText(CHANNELS[0].displayName)).toBeVisible(),
		);
		await waitFor(() =>
			expect(screen.getByRole("button", { name: "Undo" })).toBeVisible(),
		);
	},
});
