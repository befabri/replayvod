import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import { CATEGORIES } from "@/test/fixtures";
import { ViewAllLink } from "./view-all-link";

const meta = preview.meta({
	title: "UI/ViewAllLink",
	component: ViewAllLink,
	args: { to: "/dashboard/categories" },
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		await expect(canvas.getByRole("link")).toHaveAttribute(
			"href",
			"/dashboard/categories",
		);
	},
});

export const InSectionHeader = meta.story({
	render: (args) => (
		<div className="flex w-[40rem] items-center justify-between gap-4">
			<h2 className="text-xl font-medium text-foreground">
				{CATEGORIES[0].name}
			</h2>
			<ViewAllLink {...args} />
		</div>
	),
});
