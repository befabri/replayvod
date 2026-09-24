import preview from "#.storybook/preview";
import { Card, CardContent, CardHeader } from "./card";
import { Skeleton } from "./skeleton";

const SHAPES = [
	{ label: "Text line", className: "h-4 w-48" },
	{ label: "Heading", className: "h-6 w-64" },
	{ label: "Circle", className: "size-10 rounded-full" },
	{ label: "Media", className: "aspect-video w-64 rounded-lg" },
];

const meta = preview.meta({
	title: "UI/Skeleton",
	component: Skeleton,
	args: { className: "h-4 w-48" },
});

export const Default = meta.story();

export const Shapes = meta.story({
	argTypes: { className: { control: false } },
	render: (args) => (
		<div className="flex flex-wrap items-end gap-6">
			{SHAPES.map((shape) => (
				<figure key={shape.label} className="flex flex-col gap-2">
					<Skeleton {...args} className={shape.className} />
					<figcaption className="text-xs text-muted-foreground">
						{shape.label}
					</figcaption>
				</figure>
			))}
		</div>
	),
});

export const InCard = meta.story({
	argTypes: { className: { control: false } },
	render: (args) => (
		<Card className="w-80">
			<CardHeader className="flex-row items-center gap-3">
				<Skeleton {...args} className="size-10 shrink-0 rounded-full" />
				<div className="flex-1 space-y-2">
					<Skeleton {...args} className="h-4 w-3/4" />
					<Skeleton {...args} className="h-3 w-1/2" />
				</div>
			</CardHeader>
			<CardContent>
				<Skeleton {...args} className="aspect-video w-full rounded-lg" />
			</CardContent>
		</Card>
	),
});
