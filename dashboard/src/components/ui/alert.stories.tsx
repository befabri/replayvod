import { HardDrivesIcon, WarningCircleIcon } from "@phosphor-icons/react";
import type { VariantProps } from "class-variance-authority";
import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { allOf } from "@/test/exhaustive";
import { CHANNELS, VOD_TITLES } from "@/test/fixtures";
import {
	Alert,
	AlertDescription,
	AlertTitle,
	type alertVariants,
} from "./alert";
import { Button } from "./button";

type Variant = NonNullable<VariantProps<typeof alertVariants>["variant"]>;

const VARIANTS = allOf<Variant>({
	default: true,
	success: true,
	destructive: true,
});

const meta = preview.meta({
	title: "UI/Alert",
	component: Alert,
	args: {
		variant: "destructive" as const,
		children: `${VOD_TITLES[0]}: connection refused`,
	},
	argTypes: {
		variant: { control: "select", options: VARIANTS },
		icon: { control: false },
		action: { control: false },
	},
	decorators: [
		(Story) => (
			<div className="w-[32rem]">
				<Story />
			</div>
		),
	],
});

export const Destructive = meta.story({
	play: async ({ canvas }) => {
		await expect(canvas.getByRole("alert")).toHaveTextContent(
			"connection refused",
		);
	},
});

export const Success = meta.story({
	args: { variant: "success" },
	play: async ({ canvas }) => {
		await expect(canvas.getByRole("status")).toBeVisible();
		await expect(canvas.queryByRole("alert")).toBeNull();
	},
});

export const Variants = meta.story({
	render: (args) => (
		<div className="space-y-3">
			{VARIANTS.map((variant) => (
				<Alert key={variant} {...args} variant={variant}>
					{variant}
				</Alert>
			))}
		</div>
	),
});

export const WithRetry = meta.story({
	args: {
		action: (
			<Button variant="outline" size="sm" onClick={fn()}>
				Retry
			</Button>
		),
	},
});

export const WithTitleAndIcon = meta.story({
	args: {
		icon: <WarningCircleIcon weight="fill" />,
		children: (
			<>
				<AlertTitle>{CHANNELS[0].displayName}</AlertTitle>
				<AlertDescription>{VOD_TITLES[1]}</AlertDescription>
			</>
		),
	},
});

export const Neutral = meta.story({
	args: {
		variant: "default",
		icon: <HardDrivesIcon weight="fill" />,
		children: (
			<>
				<AlertTitle>{CHANNELS[1].displayName}</AlertTitle>
				<AlertDescription>{VOD_TITLES[2]}</AlertDescription>
			</>
		),
		action: (
			<Button variant="link" size="inline" onClick={fn()}>
				Open
			</Button>
		),
	},
});
