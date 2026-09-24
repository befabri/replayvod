import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { CATEGORIES, makeVideos } from "@/test/fixtures";
import { effectiveOpacity } from "@/test/opacity";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./tabs";

const TABS = CATEGORIES.slice(0, 3);
const VIDEOS = makeVideos(60);

function CategoryPanels() {
	return TABS.map((category) => (
		<TabsContent key={category.id} value={category.id} className="pt-4">
			<ul className="space-y-1 text-sm">
				{VIDEOS.filter((video) => video.primary_category_id === category.id)
					.slice(0, 3)
					.map((video) => (
						<li key={video.id} className="truncate">
							{video.title}
						</li>
					))}
			</ul>
		</TabsContent>
	));
}

const meta = preview.meta({
	title: "UI/Tabs",
	component: Tabs,
	args: { defaultValue: TABS[0].id, onValueChange: fn() },
	render: (args) => (
		<Tabs {...args} className="w-96">
			<TabsList>
				{TABS.map((category) => (
					<TabsTrigger key={category.id} value={category.id}>
						{category.name}
					</TabsTrigger>
				))}
			</TabsList>
			<CategoryPanels />
		</Tabs>
	),
});

export const Default = meta.story();

export const SecondTabActive = meta.story({
	args: { defaultValue: TABS[1].id },
});

export const FullWidth = meta.story({
	render: (args) => (
		<Tabs {...args} className="w-md">
			<TabsList className="grid w-full grid-cols-3">
				{TABS.map((category) => (
					<TabsTrigger
						key={category.id}
						value={category.id}
						className="cursor-pointer"
					>
						{category.name}
					</TabsTrigger>
				))}
			</TabsList>
			<CategoryPanels />
		</Tabs>
	),
});

export const WithDisabledTab = meta.story({
	render: (args) => (
		<Tabs {...args}>
			<TabsList>
				{TABS.map((category, index) => (
					<TabsTrigger
						key={category.id}
						value={category.id}
						disabled={index === TABS.length - 1}
					>
						{category.name}
					</TabsTrigger>
				))}
			</TabsList>
		</Tabs>
	),
	play: async ({ canvas }) => {
		const enabled = canvas.getByRole("tab", { name: TABS[0].name });
		const disabled = canvas.getByRole("tab", {
			name: TABS[TABS.length - 1].name,
		});
		await expect(enabled).not.toHaveAttribute("aria-disabled", "true");
		await expect(disabled).toHaveAttribute("aria-disabled", "true");
		await expect(effectiveOpacity(disabled)).toBeLessThan(
			effectiveOpacity(enabled),
		);
	},
});
