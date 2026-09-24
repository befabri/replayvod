import { ListIcon, SquaresFourIcon } from "@phosphor-icons/react";
import type { StoryContext } from "@storybook/react-vite";
import type { ComponentProps, ReactNode } from "react";
import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { CATEGORIES, VIDEO_QUALITIES } from "@/test/fixtures";
import { useStoryArg } from "@/test/story-args";
import { ToggleGroup, ToggleGroupItem } from "./toggle-group";

type Item = { value: string; label: string; icon?: ReactNode };

const CATEGORY_ITEMS: Item[] = CATEGORIES.slice(0, 3).map((category) => ({
	value: category.id,
	label: category.name,
}));

const ICON_ITEMS: Item[] = [
	{
		value: "grid",
		label: "Grid",
		icon: <SquaresFourIcon className="size-4" />,
	},
	{ value: "list", label: "List", icon: <ListIcon className="size-4" /> },
];

const QUALITY_ITEMS: Item[] = VIDEO_QUALITIES.map((quality) => ({
	value: quality,
	label: quality,
}));

function withItems(items: readonly Item[]) {
	return function Render(
		args: ComponentProps<typeof ToggleGroup>,
		context: Pick<StoryContext, "id">,
	) {
		const [value, setValue] = useStoryArg(args, "value", context);
		return (
			<ToggleGroup
				{...args}
				value={value}
				onValueChange={(next, details) => {
					args.onValueChange?.(next, details);
					setValue(next);
				}}
			>
				{items.map((item) => (
					<ToggleGroupItem key={item.value} value={item.value}>
						{item.icon}
						{item.label}
					</ToggleGroupItem>
				))}
			</ToggleGroup>
		);
	};
}

const meta = preview.meta({
	title: "UI/ToggleGroup",
	component: ToggleGroup,
	args: {
		value: [CATEGORY_ITEMS[0].value],
		onValueChange: fn(),
		"aria-label": "Category",
	},
	render: withItems(CATEGORY_ITEMS),
});

export const Default = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		const second = canvas.getByRole("button", {
			name: CATEGORY_ITEMS[1].label,
		});
		await userEvent.click(second);
		await expect(args.onValueChange).toHaveBeenCalledWith(
			[CATEGORY_ITEMS[1].value],
			expect.anything(),
		);
		await expect(second).toHaveAttribute("aria-pressed", "true");
		await expect(
			canvas.getByRole("button", { name: CATEGORY_ITEMS[0].label }),
		).toHaveAttribute("aria-pressed", "false");
	},
});

export const WithIcons = meta.story({
	args: { value: [ICON_ITEMS[0].value], "aria-label": "Layout" },
	render: withItems(ICON_ITEMS),
});

export const Multiple = meta.story({
	args: {
		multiple: true,
		value: [QUALITY_ITEMS[0].value, QUALITY_ITEMS[3].value],
		"aria-label": "Quality",
	},
	render: withItems(QUALITY_ITEMS),
});

export const Disabled = meta.story({
	args: { disabled: true },
});
