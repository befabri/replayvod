import {
	ArrowClockwiseIcon,
	FilmSlateIcon,
	WarningCircleIcon,
} from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { fn } from "storybook/test";
import preview from "#.storybook/preview";
import { CHANNELS } from "@/test/fixtures";
import { Button, buttonVariants } from "./button";
import { EmptyState } from "./empty-state";

const meta = preview.meta({
	title: "UI/EmptyState",
	component: EmptyState,
	args: {
		icon: <FilmSlateIcon weight="duotone" />,
		title: "Nothing here yet",
		description: `${CHANNELS[0].displayName} has no recordings.`,
		action: (
			<Button onClick={fn()}>
				<ArrowClockwiseIcon data-icon="inline-start" />
				Refresh
			</Button>
		),
	},
	argTypes: {
		icon: { control: false },
		action: { control: false },
	},
});

export const Default = meta.story();

export const WithoutAction = meta.story({
	args: { action: undefined },
});

export const WithLink = meta.story({
	args: {
		icon: <WarningCircleIcon weight="duotone" />,
		title: "Not found",
		description: undefined,
		action: (
			<Link to="/" className={buttonVariants()}>
				Home
			</Link>
		),
		className: "w-full max-w-md",
	},
});

export const TitleOnly = meta.story({
	args: {
		icon: undefined,
		description: undefined,
		action: undefined,
	},
});
