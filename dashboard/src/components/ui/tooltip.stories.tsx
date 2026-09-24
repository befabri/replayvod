import { InfoIcon } from "@phosphor-icons/react";
import type { ComponentProps } from "react";
import { expect, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import { allOf } from "@/test/exhaustive";
import { CATEGORIES, CHANNELS } from "@/test/fixtures";
import { Avatar } from "./avatar";
import { Button } from "./button";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "./tooltip";

type Side = NonNullable<ComponentProps<typeof TooltipContent>["side"]>;

const SIDES = allOf<Side>({
	top: true,
	right: true,
	bottom: true,
	left: true,
	"inline-start": true,
	"inline-end": true,
});

const CHANNEL = CHANNELS[0];

const meta = preview.meta({
	title: "UI/Tooltip",
	component: Tooltip,
	decorators: [
		(Story) => (
			<TooltipProvider>
				<div className="p-12">
					<Story />
				</div>
			</TooltipProvider>
		),
	],
});

export const Default = meta.story({
	render: (args) => (
		<div className="flex items-center gap-2 text-sm">
			<Avatar
				src={CHANNEL.profileImageUrl}
				name={CHANNEL.displayName}
				size="sm"
			/>
			<span className="font-medium">{CHANNEL.displayName}</span>
			<Tooltip {...args}>
				<TooltipTrigger
					render={
						<button
							type="button"
							className="text-muted-foreground hover:text-foreground"
							aria-label={`About ${CHANNEL.displayName}`}
						>
							<InfoIcon className="size-3.5" />
						</button>
					}
				/>
				<TooltipContent>@{CHANNEL.login}</TooltipContent>
			</Tooltip>
		</div>
	),
	play: async ({ canvas, userEvent }) => {
		await userEvent.hover(
			canvas.getByRole("button", { name: `About ${CHANNEL.displayName}` }),
		);
		await waitFor(() =>
			expect(screen.getByText(`@${CHANNEL.login}`)).toBeVisible(),
		);
	},
});

export const Sides = meta.story({
	render: (args) => (
		<div className="flex flex-wrap items-center gap-2">
			{SIDES.map((side) => (
				<Tooltip key={side} {...args}>
					<TooltipTrigger
						render={
							<Button variant="outline" size="sm">
								{side}
							</Button>
						}
					/>
					<TooltipContent side={side}>{side}</TooltipContent>
				</Tooltip>
			))}
		</div>
	),
});

export const TrackCursor = meta.story({
	args: { trackCursorAxis: "x" },
	render: (args) => (
		<div className="flex w-96 gap-px overflow-hidden rounded-full">
			{CATEGORIES.slice(0, 4).map((category) => (
				<Tooltip key={category.id} {...args}>
					<TooltipTrigger
						render={<div className="h-2.5 flex-1 bg-primary/70" />}
					/>
					<TooltipContent side="top">{category.name}</TooltipContent>
				</Tooltip>
			))}
		</div>
	),
});
