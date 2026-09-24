import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { CATEGORIES, makeVideos } from "@/test/fixtures";
import { useStoryArg } from "@/test/story-args";
import { type FilterTabOption, FilterTabs } from "./filter-tabs";

const VIDEOS = makeVideos(240);

const CATEGORY_TABS: FilterTabOption[] = CATEGORIES.slice(0, 4).map(
	(category) => ({
		value: category.id,
		label: category.name,
		count: VIDEOS.filter((video) => video.primary_category_id === category.id)
			.length,
	}),
);

const meta = preview.meta({
	title: "UI/FilterTabs",
	component: FilterTabs,
	args: {
		value: CATEGORY_TABS[0].value,
		options: CATEGORY_TABS,
		onChange: fn(),
	},
	render: function Render(args, context) {
		const [value, setValue] = useStoryArg(args, "value", context);
		return (
			<FilterTabs
				{...args}
				value={value}
				onChange={(next) => {
					args.onChange(next);
					setValue(next);
				}}
			/>
		);
	},
});

export const Default = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		const second = canvas.getByRole("tab", {
			name: new RegExp(CATEGORY_TABS[1].label),
		});
		await userEvent.click(second);
		await expect(args.onChange).toHaveBeenCalledWith(CATEGORY_TABS[1].value);
		await expect(second).toHaveAttribute("aria-selected", "true");
	},
});

export const SecondActive = meta.story({
	args: { value: CATEGORY_TABS[1].value },
});

export const LargeCounts = meta.story({
	args: {
		options: CATEGORY_TABS.map((option, index) => ({
			...option,
			count: (option.count ?? 0) * 1000 + index,
		})),
	},
});

export const WithoutCounts = meta.story({
	args: {
		options: CATEGORY_TABS.map(({ value, label }) => ({ value, label })),
	},
});

export const Overflowing = meta.story({
	args: {
		options: CATEGORIES.map((category) => ({
			value: category.id,
			label: category.name,
		})),
	},
	decorators: [
		(Story) => (
			<div className="w-80">
				<Story />
			</div>
		),
	],
});
